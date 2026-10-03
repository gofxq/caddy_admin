package domain

import (
	"context"
	"net"
	"net/netip"
)

// RuntimeCheck verifies configuration, not upstream/application health.
type RuntimeCheck struct {
	DeploymentID string `json:"deployment_id"`
	Version      int64  `json:"version"`
	Status       string `json:"status"`
	Reachable    bool   `json:"reachable"`
	ExpectedHash string `json:"expected_hash"`
	RuntimeHash  string `json:"runtime_hash"`
	CheckedAt    string `json:"checked_at"`
	Message      string `json:"message"`
}
type ServiceRelease struct {
	ID         string `json:"id"`
	Version    int64  `json:"version"`
	Status     string `json:"status"`
	Created    string `json:"created"`
	Finished   string `json:"finished"`
	RollbackID string `json:"rollback_id"`
}
type ServiceDetail struct {
	ID                string           `json:"id"`
	Revision          int64            `json:"revision"`
	Draft             *Service         `json:"draft"`
	Published         *Service         `json:"published"`
	DraftDomain       *ManagedDomain   `json:"draft_domain"`
	PublishedDomain   *ManagedDomain   `json:"published_domain"`
	Runtime           RuntimeCheck     `json:"runtime"`
	RecentDeployments []ServiceRelease `json:"recent_deployments"`
	ExternalCaddy     bool             `json:"external_caddy"`
}
type UpstreamCheck struct {
	DeploymentID string `json:"deployment_id"`
	Status       string `json:"status"`
	Target       string `json:"target"`
	DurationMS   int64  `json:"duration_ms"`
	CheckedAt    string `json:"checked_at"`
	ExpectedHash string `json:"expected_hash"`
	Vantage      string `json:"vantage"`
	Message      string `json:"message"`
}

// ValidateServiceDial reuses normalization/security rules with the published
// address snapshot. It never resolves a business hostname to a new address.
func ValidateServiceDial(policy TargetPolicy, service Service) error {
	host, _, err := net.SplitHostPort(service.Dial)
	if err != nil {
		return Invalid("已发布上游地址快照无效，请重新校验并发布")
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return Invalid("已发布上游地址快照必须为数字 IP")
	}
	policy.Resolver = dialSnapshotResolver{address: address}
	normalized, err := NormalizeService(context.Background(), policy, service)
	if err != nil {
		return err
	}
	if normalized.Dial != service.Dial {
		return Invalid("已发布上游地址快照与服务不一致，请重新校验并发布")
	}
	return nil
}

type dialSnapshotResolver struct{ address netip.Addr }

func (r dialSnapshotResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{r.address}, nil
}
