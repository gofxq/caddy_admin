package cloudflare

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

const apiURL = "https://api.cloudflare.com/client/v4"

type DNS struct{ client *http.Client }

func New() *DNS {
	return &DNS{client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl,omitempty"`
}

func (d *DNS) request(ctx context.Context, token, method, path string, body any, result any) error {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiURL+path, bytes.NewReader(raw))
	if err != nil {
		return domain.Invalid("Cloudflare 请求无效")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return &domain.AppError{Status: 503, Code: "cloudflare_unavailable", Message: "Cloudflare 暂时不可达；请检查网络后重试，已确认的 DNS 操作会先核对结果"}
	}
	defer resp.Body.Close()
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Info    struct {
			TotalPages int `json:"total_pages"`
		} `json:"result_info"`
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &envelope) != nil {
		return domain.Invalid("Cloudflare 响应无效；请重新检查，不会显示原始响应")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !envelope.Success {
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return domain.Invalid("Cloudflare Token 无效或权限不足；需要目标域名的 Zone Read、DNS Edit 权限")
		}
		return &domain.AppError{Status: 503, Code: "cloudflare_api", Message: "Cloudflare 操作未完成；请检查 Token 权限、记录冲突或稍后重试"}
	}
	if envelope.Info.TotalPages > 1 {
		return domain.Invalid("Cloudflare 返回过多匹配记录；请先清理重复 DNS 记录")
	}
	if result != nil && json.Unmarshal(envelope.Result, result) != nil {
		return domain.Invalid("Cloudflare 记录响应无效")
	}
	return nil
}
func (d *DNS) records(ctx context.Context, token, zone, name string) ([]record, error) {
	result := []record{}
	err := d.request(ctx, token, http.MethodGet, "/zones/"+url.PathEscape(zone)+"/dns_records?"+url.Values{"name.exact": {name}, "per_page": {"100"}}.Encode(), nil, &result)
	for _, r := range result {
		if !strings.EqualFold(strings.TrimSuffix(r.Name, "."), name) {
			return nil, domain.Invalid("Cloudflare 返回了不匹配的 DNS 记录；未修改记录，请重新检查")
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, err
}
func (d *DNS) Preview(ctx context.Context, token, hostname, address string) (application.SetupDNSPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := application.SetupDNSPlan{}
	if err := domain.ValidateCloudflareToken(token); err != nil {
		return p, err
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil || !ip.IsPrivate() || ip.IsLoopback() || ip.Is4In6() {
		return p, domain.Invalid("自动 DNS 配置需要可访问的私网 IPv4 或 IPv6 地址，不含协议或端口")
	}
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	if !domain.ValidDomain(hostname) || strings.ContainsAny(hostname, "/*: ?#\\") || !strings.Contains(hostname, ".") {
		return p, domain.Invalid("Homelab 域名无效")
	}
	// Exact suffix queries prevent selecting an unrelated zone visible to the token.
	labels := strings.Split(hostname, ".")
	for i := 0; i < len(labels)-1; i++ {
		name := strings.Join(labels[i:], ".")
		var zones []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err = d.request(ctx, token, http.MethodGet, "/zones?"+url.Values{"name": {name}, "per_page": {"50"}}.Encode(), nil, &zones); err != nil {
			return p, err
		}
		for _, z := range zones {
			if z.Name == name && z.ID != "" {
				p.ZoneID = z.ID
				break
			}
		}
		if p.ZoneID != "" {
			break
		}
	}
	if p.ZoneID == "" {
		return p, domain.Invalid("Token 无法访问对应 Cloudflare Zone；请确认域名已托管并授予该域名权限")
	}
	p.Name = "*." + hostname
	p.Address = ip.String()
	p.Type = "AAAA"
	if ip.Is4() {
		p.Type = "A"
	}
	records, err := d.records(ctx, token, p.ZoneID, p.Name)
	if err != nil {
		return p, err
	}
	admin, err := d.records(ctx, token, p.ZoneID, "caddyadmin."+hostname)
	if err != nil {
		return p, err
	}
	hasAdminAddress := false
	for _, r := range admin {
		if r.Type == p.Type && r.Content == p.Address && !r.Proxied {
			hasAdminAddress = true
		}
		if r.Type == "NS" || r.Type == "CNAME" || ((r.Type == "A" || r.Type == "AAAA") && (r.Type != p.Type || r.Content != p.Address || r.Proxied)) {
			return p, domain.Invalid("已有控制台精确 DNS 记录覆盖通配符且与目标地址不一致；请先核对该记录")
		}
	}
	if len(admin) > 0 && !hasAdminAddress {
		return p, domain.Invalid("控制台精确名称已有记录但缺少匹配地址，通配符不会生效；请先处理该记录")
	}
	p.Action = "create"
	same := 0
	for _, r := range records {
		if r.Type == "CNAME" {
			return p, domain.Invalid("通配符已有 CNAME 记录；请先处理冲突，不会自动覆盖")
		}
		if r.Type == p.Type {
			same++
			p.RecordID = r.ID
			p.OldAddress = r.Content
			p.OldProxied = r.Proxied
			p.Action = "update"
			if r.Content == p.Address && !r.Proxied {
				p.Action = "reuse"
			}
		} else if r.Type == "A" || r.Type == "AAAA" {
			p.Warning = "保留已有另一地址类型的记录；设备可能使用该地址，请确认其可达性"
		}
	}
	if same > 1 {
		return p, domain.Invalid("通配符存在多条同类型记录；请先处理重复记录")
	}
	raw, _ := json.Marshal(struct {
		Zone, Name, Type, Address string
		Records, Admin            []record
	}{p.ZoneID, p.Name, p.Type, p.Address, records, admin})
	sum := sha256.Sum256(raw)
	p.Fingerprint = hex.EncodeToString(sum[:])
	unaffected := []record{}
	for _, r := range records {
		if r.Type != p.Type {
			unaffected = append(unaffected, r)
		}
	}
	raw, _ = json.Marshal(struct {
		Zone, Name, Type, Address string
		Records, Admin            []record
	}{p.ZoneID, p.Name, p.Type, p.Address, unaffected, admin})
	sum = sha256.Sum256(raw)
	p.ContextFingerprint = hex.EncodeToString(sum[:])
	return p, nil
}
func (d *DNS) Apply(ctx context.Context, token string, p application.SetupDNSPlan) error {
	current, err := d.Preview(ctx, token, strings.TrimPrefix(p.Name, "*."), p.Address)
	if err != nil {
		return err
	}
	if current.ContextFingerprint != p.ContextFingerprint {
		return domain.Conflict("DNS 记录已变化，请重新预览并确认")
	}
	if current.Action == "reuse" && (p.Action != "reuse" || current.Fingerprint == p.Fingerprint) {
		return nil
	}
	if current.Fingerprint != p.Fingerprint {
		return domain.Conflict("DNS 记录已变化，请重新预览并确认")
	}
	body := record{Type: p.Type, Name: p.Name, Content: p.Address, Proxied: false}
	method := http.MethodPost
	path := "/zones/" + url.PathEscape(p.ZoneID) + "/dns_records"
	if p.Action == "update" {
		method = http.MethodPatch
		path += "/" + url.PathEscape(p.RecordID)
	} else {
		body.TTL = 1
	}
	writeErr := d.request(ctx, token, method, path, body, nil)
	// A lost response does not imply that a write failed. Read back before retrying.
	after, readErr := d.Preview(ctx, token, strings.TrimPrefix(p.Name, "*."), p.Address)
	if readErr == nil && after.Action == "reuse" && after.ContextFingerprint == p.ContextFingerprint {
		return nil
	}
	if writeErr != nil {
		return writeErr
	}
	if readErr != nil {
		return readErr
	}
	return &domain.AppError{Status: 503, Code: "cloudflare_dns_uncertain", Message: "DNS 写入结果待核对；保留初始化信息后重试，将先读取实际记录"}
}
