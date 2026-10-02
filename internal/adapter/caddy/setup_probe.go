package caddy

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"crypto/tls"
	"crypto/x509"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
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
	return &net.Resolver{PreferGo: true, StrictErrors: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
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

func (*SetupProbe) DNSMatches(ctx context.Context, homelab, address string, resolvers []string) error {
	return checkSetupDNS(ctx, homelab, address, resolvers, lookupSetupDNS)
}

func checkSetupDNS(ctx context.Context, homelab, address string, resolvers []string, lookup func(context.Context, string, string) ([]netip.Addr, error)) error {
	if address == "" {
		return domain.Invalid("请填写有效的目标 IP 和 DNS 解析器")
	}
	report, err := setupDNSResults(ctx, homelab, address, resolvers, lookup)
	if err != nil {
		return err
	}
	for _, query := range report.Queries {
		if query.Status == "block" {
			return &domain.AppError{Status: 409, Code: "setup_dns_not_ready", Message: fmt.Sprintf("%s：%s 经 %s 应解析到 %s；DNS 变更不会自动撤销，请检查记录、解析器或缓存后重试", query.Message, query.Name, query.Resolver, address)}
		}
	}
	return nil
}
func (*SetupProbe) DNSResults(ctx context.Context, homelab, address string, resolvers []string) (application.SetupDNSReport, error) {
	return setupDNSResults(ctx, homelab, address, resolvers, lookupSetupDNS)
}
func setupDNSResults(ctx context.Context, homelab, address string, resolvers []string, lookup func(context.Context, string, string) ([]netip.Addr, error)) (application.SetupDNSReport, error) {
	target, err := netip.ParseAddr(address)
	if (address != "" && err != nil) || len(resolvers) == 0 {
		return application.SetupDNSReport{}, domain.Invalid("请填写有效的目标 IP 和 DNS 解析器")
	}
	nonce := make([]byte, 12)
	if _, err = rand.Read(nonce); err != nil {
		return application.SetupDNSReport{}, domain.Invalid("无法生成 DNS 检查名称，请重试")
	}
	names := []string{"caddyadmin." + homelab + ".", "setup-check-" + hex.EncodeToString(nonce) + "." + homelab + "."}
	report := application.SetupDNSReport{Verified: true, Queries: make([]application.SetupDNSQuery, len(resolvers)*len(names))}
	type result struct {
		index int
		query application.SetupDNSQuery
	}
	results := make(chan result, len(report.Queries))
	for resolverIndex, resolver := range resolvers {
		for nameIndex, name := range names {
			index := resolverIndex*len(names) + nameIndex
			go func() {
				found, lookupErr := lookup(ctx, name, resolver)
				query := application.SetupDNSQuery{Name: name, Resolver: resolver, Addresses: []string{}, Status: "pass", Message: "全部解析地址与目标 IP 一致"}
				for _, ip := range found {
					query.Addresses = append(query.Addresses, ip.Unmap().String())
				}
				if address == "" {
					query.Message = "解析成功（未核对目标 IP）"
				}
				message := ""
				if lookupErr != nil {
					if dnsErr, ok := lookupErr.(*net.DNSError); ok && dnsErr.IsNotFound {
						message = "解析尚未生效"
					} else {
						message = "DNS 检查失败，请检查解析器连接后重试"
					}
				} else if len(found) == 0 {
					message = "解析尚未生效"
				} else if address != "" {
					for _, ip := range found {
						if ip.Unmap() != target.Unmap() {
							message = "解析地址不符"
							break
						}
					}
				}
				if message != "" {
					query.Status = "block"
					query.Message = message
				}
				results <- result{index, query}
			}()
		}
	}
	for range report.Queries {
		result := <-results
		report.Queries[result.index] = result.query
		if result.query.Status == "block" {
			report.Verified = false
		}
	}
	return report, nil
}
