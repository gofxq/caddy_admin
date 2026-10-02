package caddy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

type SetupProbe struct {
	RootCAs *x509.CertPool
}

func resolverAt(address string) *net.Resolver {
	endpoint := resolverEndpoint(address)
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, endpoint)
	}}
}

func resolverEndpoint(address string) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	return net.JoinHostPort(address, "53")
}

func lookupWithResolver(ctx context.Context, name string, addresses []string) ([]netip.Addr, bool) {
	return lookupResolvers(ctx, name, addresses, func(ctx context.Context, name, address string) ([]netip.Addr, error) {
		return resolverAt(address).LookupNetIP(ctx, "ip", name)
	})
}

func lookupResolvers(ctx context.Context, name string, addresses []string, lookup func(context.Context, string, string) ([]netip.Addr, error)) ([]netip.Addr, bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		addresses []netip.Addr
		responded bool
	}
	results := make(chan result, len(addresses))
	pending := 0
	for _, address := range addresses {
		if _, err := netip.ParseAddr(address); err != nil {
			host, _, splitErr := net.SplitHostPort(address)
			if splitErr != nil || net.ParseIP(host) == nil {
				continue
			}
		}
		pending++
		go func() {
			found, err := lookup(ctx, name, address)
			dnsError, notFound := err.(*net.DNSError)
			results <- result{found, err == nil || notFound && dnsError.IsNotFound}
		}()
	}
	responded := false
	for range pending {
		select {
		case value := <-results:
			responded = responded || value.responded
			if len(value.addresses) > 0 {
				return value.addresses, true
			}
		case <-ctx.Done():
			return nil, responded
		}
	}
	return nil, responded
}

func (*SetupProbe) ResolverReachable(ctx context.Context, resolvers []string) bool {
	_, responded := lookupWithResolver(ctx, "resolver-check.invalid", resolvers)
	return responded
}

func (*SetupProbe) AdminDNS(ctx context.Context, adminDomain string, resolvers []string) bool {
	found, _ := lookupWithResolver(ctx, adminDomain, resolvers)
	return len(found) > 0
}

func (probe *SetupProbe) ExternalConsole(ctx context.Context, origin, probeAddress string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return false
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", probeAddress)
		},
		TLSClientConfig: &tls.Config{ServerName: parsed.Hostname(), RootCAs: probe.RootCAs, MinVersion: tls.VersionTLS12},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 2 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(origin, "/")+"/api/v1/auth/session", nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusUnauthorized
}

func ResolverSuggestions(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return []string{}
	}
	defer file.Close()
	result := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() && len(result) < 3 {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || fields[0] != "nameserver" {
			continue
		}
		if address, parseErr := netip.ParseAddr(fields[1]); parseErr == nil {
			result = append(result, address.String())
		}
	}
	return result
}
