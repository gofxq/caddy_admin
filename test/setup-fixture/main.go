// Isolated acceptance fixture only. It implements the fixed Cloudflare HTTPS
// target and a local DNS resolver without contacting Cloudflare or a public CA.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	address := net.ParseIP(os.Getenv("FIXTURE_ADDRESS")).To4()
	if address == nil {
		log.Fatal("fixture address missing")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal("key generation failed")
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Isolated setup fixture"}, DNSNames: []string{"api.cloudflare.com", "cloudflare-dns.com"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		log.Fatal("certificate generation failed")
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err = os.WriteFile("/fixture/ca.crt", public, 0644); err != nil {
		log.Fatal("certificate write failed")
	}
	pair, err := tls.X509KeyPair(public, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		log.Fatal("TLS fixture failed")
	}
	var reads, writes atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/stats" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"reads": reads.Load(), "writes": writes.Load()})
			return
		}
		if r.URL.Path == "/dns-query" {
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				w.WriteHeader(403)
				return
			}
			name := r.URL.Query().Get("name")
			kind := 1
			if r.URL.Query().Get("type") == "AAAA" {
				kind = 28
			}
			answers := []map[string]any{}
			if kind == 1 {
				answers = append(answers, map[string]any{"name": name, "type": kind, "data": address.String()})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Status": 0, "Question": []map[string]any{{"name": name, "type": kind}}, "Answer": answers})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 40) {
			w.WriteHeader(403)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false})
			return
		}
		if r.Method != "GET" {
			writes.Add(1)
			w.WriteHeader(403)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false})
			return
		}
		reads.Add(1)
		var result any = []any{}
		switch r.URL.Path {
		case "/client/v4/zones":
			result = []map[string]any{{"id": "test-zone", "name": r.URL.Query().Get("name")}}
		case "/client/v4/zones/test-zone/dns_records":
			name := r.URL.Query().Get("name.exact")
			if strings.HasPrefix(name, "*.") {
				result = []map[string]any{{"id": "record", "type": "A", "name": name, "content": address.String(), "proxied": false, "ttl": 120}}
			}
		default:
			w.WriteHeader(404)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	})
	go serveDNS(address)
	server := &http.Server{Addr: ":443", Handler: mux, ReadHeaderTimeout: 3 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}}
	if err = server.ListenAndServeTLS("", ""); err != nil {
		log.Fatal("fixture listener failed")
	}
}
func serveDNS(address net.IP) {
	socket, err := net.ListenPacket("udp", ":53")
	if err != nil {
		log.Fatal("DNS fixture failed")
	}
	defer socket.Close()
	packet := make([]byte, 2048)
	for {
		n, remote, err := socket.ReadFrom(packet)
		if err != nil {
			return
		}
		if n < 17 {
			continue
		}
		end := 12
		for end < n && packet[end] != 0 {
			end += int(packet[end]) + 1
		}
		end++
		if end+4 > n {
			continue
		}
		kind := binary.BigEndian.Uint16(packet[end : end+2])
		question := packet[12 : end+4]
		response := append([]byte{}, packet[:12]...)
		binary.BigEndian.PutUint16(response[2:4], 0x8180)
		binary.BigEndian.PutUint16(response[6:8], 0)
		binary.BigEndian.PutUint16(response[8:10], 0)
		binary.BigEndian.PutUint16(response[10:12], 0)
		response = append(response, question...)
		if kind == 1 {
			binary.BigEndian.PutUint16(response[6:8], 1)
			response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 1, 0, 4)
			response = append(response, address...)
		}
		_, _ = socket.WriteTo(response, remote)
	}
}
