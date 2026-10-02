package caddy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

const bootstrapCertificateLifetime = 7 * 24 * time.Hour

type BootstrapTLS struct{}

func (*BootstrapTLS) Ensure(certPath, keyPath, origin string) error {
	return EnsureBootstrapCertificate(certPath, keyPath, origin)
}

func (*BootstrapTLS) Remove(certPath, keyPath string) error {
	for _, path := range []string{certPath, keyPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func EnsureBootstrapCertificate(certPath, keyPath, origin string) error {
	originURL, err := url.Parse(origin)
	if err != nil || originURL.Scheme != "https" || originURL.Hostname() == "" || originURL.User != nil || originURL.Path != "" || originURL.RawQuery != "" || originURL.ForceQuery || originURL.Fragment != "" {
		return domain.Invalid("初始化 Origin 必须是有效的 HTTPS 地址")
	}
	if certPath == "" || keyPath == "" || filepath.Dir(certPath) != filepath.Dir(keyPath) {
		return domain.Invalid("初始化证书路径无效")
	}
	for _, path := range []string{certPath, keyPath} {
		info, statErr := os.Lstat(path)
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
			return domain.Invalid("初始化证书路径必须是普通文件")
		}
	}
	certInfo, certErr := os.Lstat(certPath)
	keyInfo, keyErr := os.Lstat(keyPath)
	certExists, keyExists := certErr == nil, keyErr == nil
	if certExists != keyExists {
		return fmt.Errorf("初始化证书和私钥不完整")
	}
	if certExists {
		if certInfo.Mode().Perm() != 0600 || keyInfo.Mode().Perm() != 0600 {
			return domain.Invalid("初始化证书和私钥必须为 0600 权限")
		}
		pair, loadErr := tls.LoadX509KeyPair(certPath, keyPath)
		if loadErr != nil {
			return domain.Invalid("初始化证书或私钥无法读取")
		}
		leaf, parseErr := x509.ParseCertificate(pair.Certificate[0])
		if parseErr != nil {
			return domain.Invalid("初始化证书无法解析")
		}
		if leaf.VerifyHostname(originURL.Hostname()) == nil && time.Now().Before(leaf.NotAfter) && time.Now().After(leaf.NotBefore) {
			return nil
		}
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: originURL.Hostname()}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(bootstrapCertificateLifetime), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	if ip := net.ParseIP(originURL.Hostname()); ip != nil {
		leaf.IPAddresses = []net.IP{ip}
	} else {
		leaf.DNSNames = []string{originURL.Hostname()}
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	if err = AtomicWrite(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})); err != nil {
		return err
	}
	return AtomicWrite(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}))
}
