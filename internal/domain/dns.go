package domain

import "regexp"

type DNSProvider string

const (
	DNSProviderCloudflare DNSProvider = "cloudflare"
	DNSProviderDNSPod     DNSProvider = "dnspod"
)

func NormalizeDNSProvider(p DNSProvider) (DNSProvider, error) {
	if p == "" {
		return DNSProviderCloudflare, nil
	}
	if p != DNSProviderCloudflare && p != DNSProviderDNSPod {
		return "", Invalid("不支持的 DNS 服务商")
	}
	return p, nil
}

var dnspodTokenPattern = regexp.MustCompile(`^[0-9]+,[A-Za-z0-9_-]{16,256}$`)

func ValidateDNSToken(p DNSProvider, token string) error {
	p, e := NormalizeDNSProvider(p)
	if e != nil {
		return e
	}
	if p == DNSProviderCloudflare {
		return ValidateCloudflareToken(token)
	}
	if !dnspodTokenPattern.MatchString(token) {
		return Invalid("DNSPod 凭据格式无效；请填写 ID,Token")
	}
	return nil
}
func IsPublicCertificateMode(m CertificateMode) bool {
	return m == CertificateModeCloudflare || m == CertificateModeDNSPod
}
