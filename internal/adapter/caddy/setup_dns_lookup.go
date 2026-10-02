package caddy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Query the selected resolver directly: /etc/hosts and search suffixes must
// never substitute for proof that the configured DNS records are effective.
func lookupSetupDNS(ctx context.Context, hostname, resolver string) ([]netip.Addr, error) {
	name, err := dnsmessage.NewName(hostname)
	if err != nil {
		return nil, err
	}
	found := []netip.Addr{}
	for _, kind := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		var id [2]byte
		if _, err = rand.Read(id[:]); err != nil {
			return found, err
		}
		question := dnsmessage.Question{Name: name, Type: kind, Class: dnsmessage.ClassINET}
		query := dnsmessage.Message{Header: dnsmessage.Header{ID: binary.BigEndian.Uint16(id[:]), RecursionDesired: true}, Questions: []dnsmessage.Question{question}}
		raw, err := query.Pack()
		if err != nil {
			return found, err
		}
		response, err := exchangeSetupDNS(ctx, resolver, "udp", raw)
		if err != nil {
			return found, err
		}
		if response.Header.Truncated {
			response, err = exchangeSetupDNS(ctx, resolver, "tcp", raw)
			if err != nil {
				return found, err
			}
		}
		if !response.Header.Response || response.Header.Truncated || response.Header.ID != query.Header.ID || len(response.Questions) != 1 || response.Questions[0] != question {
			return found, errors.New("invalid DNS response")
		}
		if response.Header.RCode == dnsmessage.RCodeNameError {
			return found, &net.DNSError{IsNotFound: true}
		}
		if response.Header.RCode != dnsmessage.RCodeSuccess {
			return found, errors.New("DNS query failed")
		}
		for _, answer := range response.Answers {
			switch body := answer.Body.(type) {
			case *dnsmessage.AResource:
				found = append(found, netip.AddrFrom4(body.A))
			case *dnsmessage.AAAAResource:
				found = append(found, netip.AddrFrom16(body.AAAA))
			}
		}
	}
	return found, nil
}

func exchangeSetupDNS(ctx context.Context, resolver, network string, query []byte) (dnsmessage.Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	response := dnsmessage.Message{}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, resolverEndpoint(resolver))
	if err != nil {
		return response, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return response, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if network == "tcp" {
		frame := make([]byte, len(query)+2)
		binary.BigEndian.PutUint16(frame, uint16(len(query)))
		copy(frame[2:], query)
		if _, err = io.Copy(conn, bytes.NewReader(frame)); err != nil {
			return response, err
		}
		var size [2]byte
		if _, err = io.ReadFull(conn, size[:]); err != nil {
			return response, err
		}
		raw := make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err = io.ReadFull(conn, raw); err != nil {
			return response, err
		}
		err = response.Unpack(raw)
	} else {
		if _, err = conn.Write(query); err != nil {
			return response, err
		}
		raw := make([]byte, 65535)
		var n int
		n, err = conn.Read(raw)
		if err == nil {
			err = response.Unpack(raw[:n])
		}
	}
	return response, err
}
