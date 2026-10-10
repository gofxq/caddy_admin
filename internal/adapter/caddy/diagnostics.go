package caddy

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/gofxq/caddy_admin/internal/domain"
	"golang.org/x/net/dns/dnsmessage"
)

type ObservationDiagnostics struct{}

func (*ObservationDiagnostics) Diagnose(ctx context.Context, settings domain.ManagedSettings, d domain.ManagedDomain) []domain.DiagnosticCheck {
	checks := []domain.DiagnosticCheck{}
	if len(settings.Resolvers) == 0 {
		return []domain.DiagnosticCheck{{Name: "DNS / CAA", Status: "unknown", Message: "尚未配置服务器 DNS", Values: []string{}}}
	}
	for _, resolver := range settings.Resolvers {
		addresses, err := lookupSetupDNS(ctx, "diagnostic-probe."+d.Name+".", resolver)
		c := domain.DiagnosticCheck{Name: "业务 wildcard DNS · " + resolver, Status: "unknown", Message: "从 Manager 查询已配置 DNS；与浏览器或外部 Caddy 视角可能不同", Values: []string{}}
		if err == nil && len(addresses) > 0 {
			c.Status = "pass"
			for _, ip := range addresses {
				c.Values = append(c.Values, ip.String())
			}
		} else {
			c.Message = "业务 wildcard 无法解析或查询超时；检查 DNS 记录和解析器连通性"
		}
		checks = append(checks, c)
		records, err := lookupCAA(ctx, d.Name, resolver)
		c = domain.DiagnosticCheck{Name: "Wildcard CAA · " + resolver, Status: "unknown", Message: "无法查询或解释有效 CAA 策略", Values: []string{}}
		if err == nil {
			c.Status, c.Message, c.Values = checkCAA(records)
		}
		checks = append(checks, c)
		if ctx.Err() != nil {
			break
		}
	}
	return checks
}

type caaRecord struct {
	Flags      byte
	Tag, Value string
}

func lookupCAA(ctx context.Context, hostname, resolver string) ([]caaRecord, error) {
	for depth := 0; depth < 12; depth++ {
		name, err := dnsmessage.NewName(hostname + ".")
		if err != nil {
			return nil, err
		}
		var id [2]byte
		if _, err = rand.Read(id[:]); err != nil {
			return nil, err
		}
		q := dnsmessage.Question{Name: name, Type: dnsmessage.Type(257), Class: dnsmessage.ClassINET}
		msg := dnsmessage.Message{Header: dnsmessage.Header{ID: binary.BigEndian.Uint16(id[:]), RecursionDesired: true}, Questions: []dnsmessage.Question{q}}
		raw, err := msg.Pack()
		if err != nil {
			return nil, err
		}
		answer, err := exchangeSetupDNS(ctx, resolver, "udp", raw)
		if err != nil {
			return nil, err
		}
		if answer.Truncated {
			answer, err = exchangeSetupDNS(ctx, resolver, "tcp", raw)
			if err != nil {
				return nil, err
			}
		}
		if !answer.Response || answer.Truncated || answer.ID != msg.ID || len(answer.Questions) != 1 || answer.Questions[0] != q || answer.RCode != dnsmessage.RCodeSuccess {
			return nil, fmt.Errorf("invalid CAA response")
		}
		out := []caaRecord{}
		for _, a := range answer.Answers {
			if a.Header.Type == dnsmessage.TypeCNAME {
				return nil, fmt.Errorf("CAA alias requires separate verification")
			}
			if a.Header.Type != dnsmessage.Type(257) || a.Header.Name != name {
				continue
			}
			body, ok := a.Body.(*dnsmessage.UnknownResource)
			if !ok || len(body.Data) < 2 || int(body.Data[1])+2 > len(body.Data) {
				return nil, fmt.Errorf("invalid CAA record")
			}
			n := int(body.Data[1])
			out = append(out, caaRecord{Flags: body.Data[0], Tag: strings.ToLower(string(body.Data[2 : 2+n])), Value: string(body.Data[2+n:])})
		}
		if len(out) > 0 {
			return out, nil
		}
		dot := strings.IndexByte(hostname, '.')
		if dot < 0 {
			return nil, nil
		}
		hostname = hostname[dot+1:]
	}
	return nil, fmt.Errorf("CAA inheritance limit")
}
func checkCAA(records []caaRecord) (string, string, []string) {
	values := []string{}
	if len(records) == 0 {
		return "pass", "未发现继承 CAA 限制；DNS 检查不证明 CA 签发一定成功", values
	}
	wildcard := false
	for _, r := range records {
		if r.Tag == "issuewild" {
			wildcard = true
		}
	}
	constrained, allowed, complex := false, false, false
	for _, r := range records {
		if r.Flags&128 != 0 && r.Tag != "issue" && r.Tag != "issuewild" && r.Tag != "iodef" {
			return "fail", "发现未知关键 CAA 标签，CA 可能拒绝签发", values
		}
		tag := "issue"
		if wildcard {
			tag = "issuewild"
		}
		if r.Tag != tag {
			continue
		}
		constrained = true
		issuer := strings.TrimSpace(strings.SplitN(strings.ToLower(r.Value), ";", 2)[0])
		if issuer == "letsencrypt.org" {
			allowed = true
		}
		if strings.Contains(r.Value, ";") {
			complex = true
		}
		if issuer == "letsencrypt.org" || issuer == "sectigo.com" || issuer == "pki.goog" || issuer == "" {
			values = append(values, tag+": "+issuer)
		} else {
			values = append(values, tag+": 其他 CA")
		}
	}
	if complex {
		return "unknown", "CAA 包含账户或校验方式限制，需人工核对与实际 ACME 账户是否匹配", values
	}
	if constrained && !allowed {
		return "fail", "Wildcard CAA 未许可当前默认 Let's Encrypt CA", values
	}
	return "pass", "CAA 未阻止默认 Let's Encrypt wildcard 签发；不证明 Token 或 ACME 可用", values
}
