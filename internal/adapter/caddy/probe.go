package caddy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"time"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type Probe struct{}

var _ application.CertificateProbe = (*Probe)(nil)

func (*Probe) Certificates(ctx context.Context, query application.CertificateQuery) []domain.Certificate {
	certificates := []domain.Certificate{}
	for _, name := range query.Domains {
		if name == "" {
			continue
		}
		probeContext, cancel := context.WithTimeout(ctx, 4*time.Second)
		certificate := domain.Certificate{Subject: "*." + name, Status: "unknown", Message: "无法检测当前呈现证书", SANs: []string{}, CheckedAt: now()}
		if query.TestTLS || query.Mode == domain.CertificateModeBootstrapInternal {
			certificate.Message = "当前使用内部引导证书，尚未启用公网可信证书"
			cancel()
			certificates = append(certificates, certificate)
			continue
		}
		serverName := "certificate-probe." + name
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 3 * time.Second}, Config: &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}}
		connection, err := dialer.DialContext(probeContext, "tcp", query.ProbeAddress)
		if err == nil {
			tlsConnection := connection.(*tls.Conn)
			chain := tlsConnection.ConnectionState().PeerCertificates
			if len(chain) > 0 {
				leaf := chain[0]
				certificate.SANs = leaf.DNSNames
				certificate.NotBefore = leaf.NotBefore.UTC().Format(time.RFC3339)
				certificate.NotAfter = leaf.NotAfter.UTC().Format(time.RFC3339)
				certificate.Days = int(time.Until(leaf.NotAfter).Hours() / 24)
				certificate.Status = "valid"
				certificate.Message = "由 Caddy 自动签发和续期"
				intermediates := x509.NewCertPool()
				for _, item := range chain[1:] {
					intermediates.AddCert(item)
				}
				_, verifyErr := leaf.Verify(x509.VerifyOptions{DNSName: serverName, Intermediates: intermediates})
				wildcard := false
				for _, san := range leaf.DNSNames {
					if san == certificate.Subject {
						wildcard = true
					}
				}
				if verifyErr != nil || !wildcard {
					certificate.Status = "invalid"
					certificate.Message = "证书信任、有效期或 wildcard 主体验证未通过"
				} else if certificate.Days < 30 {
					certificate.Status = "warning"
					certificate.Message = "证书将在 30 天内到期，请检查续期状态"
				}
			}
			_ = connection.Close()
		}
		cancel()
		certificates = append(certificates, certificate)
	}
	return certificates
}

func (probe *Probe) PublicReady(ctx context.Context, query application.CertificateQuery) bool {
	certificates := probe.Certificates(ctx, query)
	if len(certificates) == 0 {
		return false
	}
	for _, certificate := range certificates {
		if certificate.Status != "valid" && certificate.Status != "warning" {
			return false
		}
	}
	return true
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
