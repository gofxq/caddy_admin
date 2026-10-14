package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/gofxq/caddy_admin/internal/application"
)

// The endpoint is fixed; setup input cannot turn the probe into an HTTP proxy.
const setupDoHEndpoint = "https://cloudflare-dns.com/dns-query"

type DoHProbe struct{ Client *http.Client }

func (probe *DoHProbe) Check(ctx context.Context, hostname, address string) (application.SetupDNSReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	client := http.Client{Timeout: 6 * time.Second}
	if probe.Client != nil {
		client = *probe.Client
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return setupDNSResults(ctx, hostname, address, []string{setupDoHEndpoint}, func(ctx context.Context, name, _ string) ([]netip.Addr, error) {
		// A and AAAA are independent: an extra address in either family is unsafe.
		type response struct {
			addresses []netip.Addr
			err       error
		}
		results := make(chan response, 2)
		for _, kind := range []int{1, 28} {
			go func() { addresses, err := lookupDoH(ctx, &client, name, kind); results <- response{addresses, err} }()
		}
		found := []netip.Addr{}
		var failure error
		for range 2 {
			value := <-results
			found = append(found, value.addresses...)
			if value.err != nil {
				failure = value.err
			}
		}
		return found, failure
	})
}
func lookupDoH(ctx context.Context, client *http.Client, name string, kind int) ([]netip.Addr, error) {
	endpoint, _ := url.Parse(setupDoHEndpoint)
	q := endpoint.Query()
	q.Set("name", name)
	q.Set("type", map[int]string{1: "A", 28: "AAAA"}[kind])
	endpoint.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	req.Header.Set("Accept", "application/dns-json")
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DoH request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("DoH response unsuccessful")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<10+1))
	if err != nil || len(raw) > 64<<10 {
		return nil, fmt.Errorf("DoH response invalid")
	}
	var value struct {
		Status   *int `json:"Status"`
		TC       bool `json:"TC"`
		Question []struct {
			Name string `json:"name"`
			Type int    `json:"type"`
		} `json:"Question"`
		Answer []struct {
			Name string `json:"name"`
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	normalize := func(v string) string { return strings.TrimRight(strings.ToLower(v), ".") }
	if json.Unmarshal(raw, &value) != nil || value.Status == nil || value.TC || len(value.Question) != 1 || normalize(value.Question[0].Name) != normalize(name) || value.Question[0].Type != kind {
		return nil, fmt.Errorf("DoH response invalid")
	}
	if *value.Status == 3 {
		return nil, &net.DNSError{IsNotFound: true}
	}
	if *value.Status != 0 {
		return nil, fmt.Errorf("DoH lookup failed")
	}
	names := map[string]bool{normalize(name): true}
	for range len(value.Answer) {
		for _, answer := range value.Answer {
			if answer.Type == 5 && names[normalize(answer.Name)] {
				names[normalize(answer.Data)] = true
			}
		}
	}
	addresses := []netip.Addr{}
	for _, answer := range value.Answer {
		if answer.Type != kind || !names[normalize(answer.Name)] {
			continue
		}
		ip, err := netip.ParseAddr(answer.Data)
		if err != nil || ip.Zone() != "" || (kind == 1) != ip.Is4() {
			return nil, fmt.Errorf("DoH address invalid")
		}
		addresses = append(addresses, ip.Unmap())
	}
	return addresses, nil
}
