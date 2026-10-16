package dnspod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

const apiURL = "https://dnsapi.cn"

type DNS struct {
	client  *http.Client
	mu      sync.Mutex
	pending map[string]bool
}

func New() *DNS {
	return &DNS{client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type record struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"value"`
	LineID  string `json:"line_id"`
	Enabled string `json:"enabled"`
	UseAQB  string `json:"use_aqb"`
	TTL     string `json:"ttl"`
	Proxied bool   `json:"-"`
}
type envelope struct {
	Status struct {
		Code string `json:"code"`
	} `json:"status"`
	Domain struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"domain"`
	Info struct {
		Total string `json:"record_total"`
	} `json:"info"`
	Records []record `json:"records"`
}

func (d *DNS) request(ctx context.Context, token, action string, values url.Values) (envelope, error) {
	var out envelope
	values.Set("login_token", token)
	values.Set("format", "json")
	values.Set("error_on_empty", "no")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/"+action, strings.NewReader(values.Encode()))
	if err != nil {
		return out, domain.Invalid("DNSPod 请求无效")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Caddy-Web-Admin")
	resp, err := d.client.Do(req)
	if err != nil {
		return out, &domain.AppError{Status: 503, Code: "dnspod_unavailable", Message: "DNSPod 暂时不可达；请检查网络后重试，已确认的 DNS 操作会先核对结果"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &out) != nil {
		return out, domain.Invalid("DNSPod 响应无效；不会显示原始响应")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || out.Status.Code != "1" {
		apiErr := error(&domain.AppError{Status: 503, Code: "dnspod_api", Message: "DNSPod 操作未完成；请检查 ID,Token、域名权限、记录冲突或稍后重试"})
		// Only documented authentication/permission refusals from the verified
		// API in a valid 2xx envelope prove this write was never accepted.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && (action == "Record.Create" || action == "Record.Modify") && knownWriteRejection(out.Status.Code) {
			apiErr = &writeRejectedError{cause: apiErr}
		}
		return out, apiErr
	}
	return out, nil
}
func (d *DNS) records(ctx context.Context, token, zone, name string) ([]record, error) {
	result := []record{}
	expected := -1
	seen := map[string]bool{}
	for offset := 0; offset < 10000; {
		out, err := d.request(ctx, token, "Record.List", url.Values{"domain_id": {zone}, "sub_domain": {name}, "offset": {strconv.Itoa(offset)}, "length": {"100"}})
		if err != nil {
			return nil, err
		}
		total, err := strconv.Atoi(out.Info.Total)
		if err != nil || total < 0 || total > 10000 || (expected >= 0 && expected != total) {
			return nil, domain.Invalid("DNSPod 记录分页不完整或已变化，请重新检查")
		}
		expected = total
		for _, r := range out.Records {
			if r.Name != name || r.ID == "" || seen[r.ID] {
				return nil, domain.Invalid("DNSPod 返回不匹配或重复记录，请重新检查")
			}
			seen[r.ID] = true
			if r.LineID != "0" || r.Enabled != "1" || (r.UseAQB != "" && r.UseAQB != "no") {
				return nil, domain.Invalid("DNSPod 存在非默认线路、停用或安全代理记录；请先手动处理，避免覆盖错误线路")
			}
			result = append(result, r)
		}
		offset += len(out.Records)
		if offset == total {
			sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
			return result, nil
		}
		if len(out.Records) == 0 || offset > total {
			break
		}
	}
	return nil, domain.Invalid("DNSPod 记录分页不完整，请重新检查")
}
func (d *DNS) Preview(ctx context.Context, token, hostname, address string) (application.SetupDNSPlan, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p := application.SetupDNSPlan{Provider: domain.DNSProviderDNSPod}
	if err := domain.ValidateDNSToken(domain.DNSProviderDNSPod, token); err != nil {
		return p, err
	}
	address, err := domain.NormalizeSetupAddress(address)
	if err != nil {
		return p, err
	}
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	if !domain.ValidDomain(hostname) || strings.ContainsAny(hostname, "/*: ?#\\") || !strings.Contains(hostname, ".") {
		return p, domain.Invalid("通配符基础域名无效")
	}
	labels := strings.Split(hostname, ".")
	zoneName := ""
	for i := 0; i < len(labels)-1; i++ {
		name := strings.Join(labels[i:], ".")
		out, err := d.request(ctx, token, "Domain.Info", url.Values{"domain": {name}})
		if err != nil {
			if out.Status.Code == "6" {
				continue
			}
			return p, err
		}
		if out.Domain.Name != name || out.Domain.ID == "" {
			return p, domain.Invalid("DNSPod 返回不匹配域名")
		}
		p.ZoneID = out.Domain.ID
		zoneName = name
		break
	}
	if p.ZoneID == "" {
		return p, domain.Invalid("ID,Token 无法访问对应 DNSPod 域名；请确认域名已托管且凭据属于主账号")
	}
	p.Name = "*." + hostname
	p.Address = address
	p.Type = "AAAA"
	if !strings.Contains(address, ":") {
		p.Type = "A"
	}
	records, err := d.records(ctx, token, p.ZoneID, strings.TrimSuffix(p.Name, "."+zoneName))
	if err != nil {
		return p, err
	}
	admin, err := d.records(ctx, token, p.ZoneID, strings.TrimSuffix("caddyadmin."+hostname, "."+zoneName))
	if err != nil {
		return p, err
	}
	hasAdminAddress := false
	adminAddressCount := 0
	for _, r := range admin {
		if r.Type == p.Type && r.Content == p.Address && !r.Proxied {
			hasAdminAddress = true
			adminAddressCount++
		}
		if r.Type == "NS" || r.Type == "CNAME" || ((r.Type == "A" || r.Type == "AAAA") && (r.Type != p.Type || r.Content != p.Address || r.Proxied)) {
			return p, domain.Invalid("已有控制台精确 DNS 记录覆盖通配符且与目标地址不一致；请先核对该记录")
		}
	}
	if adminAddressCount > 1 {
		return p, domain.Invalid("控制台精确名称存在重复地址记录；请先处理重复记录")
	}
	if len(admin) > 0 && !hasAdminAddress {
		return p, domain.Invalid("控制台精确名称已有记录但缺少匹配地址，通配符不会生效；请先处理该记录")
	}
	p.Action = "create"
	same := 0
	for _, r := range records {
		if r.Type == "CNAME" || r.Type == "NS" {
			return p, domain.Invalid("通配符已有 CNAME 或 NS 记录；请先处理冲突，不会自动覆盖")
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
		Provider, Account, Zone, Name, Type, Address string
		Records, Admin                               []record
	}{"dnspod", tokenHash(token), p.ZoneID, p.Name, p.Type, p.Address, records, admin})
	sum := sha256.Sum256(raw)
	p.Fingerprint = hex.EncodeToString(sum[:])
	unaffected := []record{}
	for _, r := range records {
		if r.Type != p.Type {
			unaffected = append(unaffected, r)
		}
	}
	raw, _ = json.Marshal(struct {
		Provider, Account, Zone, Name, Type, Address string
		Records, Admin                               []record
	}{"dnspod", tokenHash(token), p.ZoneID, p.Name, p.Type, p.Address, unaffected, admin})
	sum = sha256.Sum256(raw)
	p.ContextFingerprint = hex.EncodeToString(sum[:])
	return p, nil
}
func (d *DNS) Apply(ctx context.Context, token string, p application.SetupDNSPlan) (applyErr error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	priorPending := d.pending[p.ContextFingerprint]
	dispatched := false
	defer func() {
		// A preflight error cannot clear an earlier uncertain write.
		if applyErr != nil && !dispatched && !priorPending {
			applyErr = &writeRejectedError{cause: applyErr}
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if p.Provider != domain.DNSProviderDNSPod {
		return domain.Conflict("DNS 服务商已变化，请重新预览")
	}
	current, err := d.Preview(ctx, token, strings.TrimPrefix(p.Name, "*."), p.Address)
	if err != nil {
		return err
	}
	if current.ContextFingerprint != p.ContextFingerprint {
		return domain.Conflict("DNS 记录已变化，请重新预览并确认")
	}
	if current.Action == "reuse" && (p.Action != "reuse" || current.Fingerprint == p.Fingerprint) {
		delete(d.pending, p.ContextFingerprint)
		return nil
	}
	if current.Fingerprint != p.Fingerprint {
		return domain.Conflict("DNS 记录已变化，请重新预览并确认")
	}
	if d.pending[p.ContextFingerprint] {
		return uncertain()
	}
	// Resolve the exact registered zone again; names sent to DNSPod are relative.
	zoneName := ""
	labels := strings.Split(strings.TrimPrefix(p.Name, "*."), ".")
	for i := 0; i < len(labels)-1; i++ {
		name := strings.Join(labels[i:], ".")
		out, e := d.request(ctx, token, "Domain.Info", url.Values{"domain": {name}})
		if e == nil && out.Domain.ID == current.ZoneID && out.Domain.Name == name {
			zoneName = name
			break
		}
	}
	if zoneName == "" {
		return domain.Conflict("DNSPod 域名已变化，请重新预览")
	}
	values := url.Values{"domain_id": {current.ZoneID}, "sub_domain": {strings.TrimSuffix(current.Name, "."+zoneName)}, "record_type": {current.Type}, "value": {current.Address}, "record_line_id": {"0"}, "ttl": {"600"}, "status": {"enable"}}
	action := "Record.Create"
	if current.Action == "update" {
		action = "Record.Modify"
		values.Set("record_id", current.RecordID)
	}
	if d.pending == nil {
		d.pending = map[string]bool{}
	}
	d.pending[p.ContextFingerprint] = true
	dispatched = true
	_, writeErr := d.request(ctx, token, action, values)
	var rejection interface{ DNSWriteRejected() bool }
	if errors.As(writeErr, &rejection) && rejection.DNSWriteRejected() {
		delete(d.pending, p.ContextFingerprint)
		return writeErr
	}
	// A lost response does not imply that a write failed. Read back before retrying.
	after, readErr := d.Preview(ctx, token, strings.TrimPrefix(p.Name, "*."), p.Address)
	if readErr == nil && after.Action == "reuse" && after.ContextFingerprint == p.ContextFingerprint {
		delete(d.pending, p.ContextFingerprint)
		return nil
	}
	if writeErr != nil {
		return writeErr
	}
	if readErr != nil {
		return readErr
	}
	return uncertain()
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func uncertain() error {
	return &domain.AppError{Status: 503, Code: "dnspod_dns_uncertain", Message: "DNS 写入结果待核对；请等待记录同步后重试，将先读取实际记录，不会重复创建"}
}

// writeRejectedError preserves the safe application error while allowing the
// use case to release its durable dispatch marker only for definite no-writes.
type writeRejectedError struct{ cause error }

func (e *writeRejectedError) Error() string          { return e.cause.Error() }
func (e *writeRejectedError) Unwrap() error          { return e.cause }
func (e *writeRejectedError) DNSWriteRejected() bool { return true }

func knownWriteRejection(code string) bool {
	// https://docs.dnspod.cn/api/common-return/ and /api/add-record/.
	// Unknown/system errors deliberately remain uncertain.
	switch code {
	case "-1", "-7", "-8", "7", "83", "85":
		return true
	}
	return false
}
