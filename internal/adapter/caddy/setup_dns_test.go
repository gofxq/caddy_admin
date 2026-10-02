package caddy

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSetupDNSRequiresEveryResolverAndEveryAddress(t *testing.T) {
	for _, tc := range []struct {
		name  string
		found []netip.Addr
		err   error
		want  string
	}{
		{"match", []netip.Addr{netip.MustParseAddr("10.0.0.6")}, nil, ""},
		{"empty", nil, nil, "尚未生效"},
		{"mismatch", []netip.Addr{netip.MustParseAddr("10.0.0.7")}, nil, "地址不符"},
		{"extra", []netip.Addr{netip.MustParseAddr("10.0.0.6"), netip.MustParseAddr("fd00::7")}, nil, "地址不符"},
		{"failure", nil, errors.New("secret raw response"), "检查失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			err := checkSetupDNS(context.Background(), "home.example.com", "10.0.0.6", []string{"10.0.0.53", "10.0.0.54"}, func(_ context.Context, name, resolver string) ([]netip.Addr, error) {
				calls.Add(1)
				if resolver == "10.0.0.54" {
					return tc.found, tc.err
				}
				return []netip.Addr{netip.MustParseAddr("10.0.0.6")}, nil
			})
			if tc.want == "" {
				if err != nil || calls.Load() != 4 {
					t.Fatal(err, calls.Load())
				}
			} else if calls.Load() != 4 || err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
		})
	}
}

func TestSetupDNSRejectsWildcardFailureDespiteCorrectConsole(t *testing.T) {
	err := checkSetupDNS(context.Background(), "home.example.com", "10.0.0.6", []string{"10.0.0.53"}, func(_ context.Context, name, _ string) ([]netip.Addr, error) {
		if name == "caddyadmin.home.example.com." {
			return []netip.Addr{netip.MustParseAddr("10.0.0.6")}, nil
		}
		if !strings.HasPrefix(name, "setup-check-") || !strings.HasSuffix(name, ".home.example.com.") {
			t.Fatal("unexpected wildcard probe", name)
		}
		return nil, &net.DNSError{IsNotFound: true}
	})
	if err == nil || !strings.Contains(err.Error(), "尚未生效") {
		t.Fatal(err)
	}
}

func TestSetupDNSUsesSelectedResolverForBothFamilies(t *testing.T) {
	for _, failAAAA := range []bool{false, true} {
		t.Run(fmt.Sprintf("AAAA_failure_%v", failAAAA), func(t *testing.T) {
			server, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				buffer := make([]byte, 4096)
				for {
					n, peer, err := server.ReadFrom(buffer)
					if err != nil {
						return
					}
					var query dnsmessage.Message
					if query.Unpack(buffer[:n]) != nil || len(query.Questions) != 1 {
						continue
					}
					question := query.Questions[0]
					response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.Header.ID, Response: true, RecursionAvailable: true}, Questions: query.Questions}
					if question.Type == dnsmessage.TypeAAAA && failAAAA {
						response.Header.RCode = dnsmessage.RCodeServerFailure
					} else if question.Type == dnsmessage.TypeA {
						response.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 1}, Body: &dnsmessage.AResource{A: [4]byte{10, 0, 0, 6}}}}
					}
					raw, err := response.Pack()
					if err == nil {
						_, _ = server.WriteTo(raw, peer)
					}
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err = (&SetupProbe{}).DNSMatches(ctx, "home.example.com", "10.0.0.6", []string{server.LocalAddr().String()})
			report, reportErr := (&SetupProbe{}).DNSResults(ctx, "home.example.com", "10.0.0.6", []string{server.LocalAddr().String()})
			server.Close()
			<-done
			if reportErr != nil || len(report.Queries) != 2 || len(report.Queries[0].Addresses) != 1 || report.Queries[0].Addresses[0] != "10.0.0.6" || report.Verified == failAAAA {
				t.Fatal("report lost A results or accepted AAAA failure", report, reportErr)
			}
			if failAAAA {
				if err == nil || !strings.Contains(err.Error(), "检查失败") {
					t.Fatal("partial A lookup passed despite AAAA SERVFAIL", err)
				}
			} else if err != nil {
				t.Fatal("correct DNS response failed", err)
			}
		})
	}
}

func TestSetupDNSReportKeepsAllResultsAndSanitizesErrors(t *testing.T) {
	report, err := setupDNSResults(context.Background(), "home.example.com", "10.0.0.6", []string{"1.1.1.1", "10.0.0.53"}, func(_ context.Context, name, resolver string) ([]netip.Addr, error) {
		if resolver == "1.1.1.1" {
			return []netip.Addr{netip.MustParseAddr("10.0.0.6"), netip.MustParseAddr("fd00::7")}, nil
		}
		if strings.HasPrefix(name, "caddyadmin.") {
			return nil, errors.New("secret raw response")
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.6")}, nil
	})
	if err != nil || report.Verified || len(report.Queries) != 4 {
		t.Fatal(report, err)
	}
	if len(report.Queries[0].Addresses) != 2 || report.Queries[0].Status != "block" || report.Queries[2].Status != "block" || report.Queries[3].Status != "pass" {
		t.Fatal(report)
	}
	for _, query := range report.Queries {
		if strings.Contains(query.Message, "secret") {
			t.Fatal(query)
		}
	}
	wildcard := report.Queries[1].Name
	if wildcard != report.Queries[3].Name || !strings.HasPrefix(wildcard, "setup-check-") || strings.Count(strings.TrimSuffix(wildcard, ".home.example.com."), ".") != 0 {
		t.Fatal(report)
	}
}
func TestSetupDNSReportWithoutTargetOnlyChecksResolution(t *testing.T) {
	for _, address := range []string{"", "10.0.0.6"} {
		report, err := setupDNSResults(context.Background(), "home.example.com", address, []string{"1.1.1.1"}, func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("10.0.0.6")}, nil
		})
		if err != nil || !report.Verified || len(report.Queries) != 2 {
			t.Fatal(report, err)
		}
		if address == "" && !strings.Contains(report.Queries[0].Message, "未核对目标 IP") {
			t.Fatal(report)
		}
	}
}
func TestSetupDNSQueriesOtherResolversDespiteTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	report, err := setupDNSResults(ctx, "home.example.com", "10.0.0.6", []string{"10.0.0.53", "1.1.1.1"}, func(ctx context.Context, _ string, resolver string) ([]netip.Addr, error) {
		if resolver == "10.0.0.53" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.6")}, nil
	})
	if err != nil || report.Verified || len(report.Queries) != 4 || report.Queries[2].Status != "pass" || report.Queries[3].Status != "pass" {
		t.Fatal(report, err)
	}
}
