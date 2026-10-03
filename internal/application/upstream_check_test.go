package application

import (
	"context"
	"errors"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type detailUpstreamProbe struct {
	target string
	check  func(context.Context, string) domain.UpstreamCheck
}

func (p *detailUpstreamProbe) Check(ctx context.Context, target string) domain.UpstreamCheck {
	p.target = target
	if p.check != nil {
		return p.check(ctx, target)
	}
	return domain.UpstreamCheck{Status: "reachable", DurationMS: 2}
}

func TestCheckUpstreamUsesPublishedSnapshot(t *testing.T) {
	service, repo, _ := detailFixture()
	probe := &detailUpstreamProbe{}
	service.upstreamProbe = probe
	repo.latest.Services[0].Host = "photos.internal"
	repo.draft.Services[0].Host = "10.42.0.9"
	repo.draft.Services[0].Dial = "10.42.0.9:2283"
	result, err := service.CheckUpstream(context.Background(), "photos", repo.latest.Hash, repo.latest.ID)
	if err != nil || result.Status != "reachable" || probe.target != "10.42.0.8:2283" || result.Target != probe.target || result.Vantage != "manager" || result.ExpectedHash != repo.latest.Hash || result.CheckedAt == "" {
		t.Fatalf("result=%+v target=%s err=%v", result, probe.target, err)
	}
	repo.draft.Services = nil
	if _, err = service.CheckUpstream(context.Background(), "photos", repo.latest.Hash, repo.latest.ID); err != nil {
		t.Fatalf("pending deletion must still check published: %v", err)
	}
}
func TestCheckUpstreamRejectsUnsafeOrUnverifiedTargets(t *testing.T) {
	for _, name := range []string{"stale", "disabled", "draft only", "drift", "uncertain", "protected", "revoked", "unlicensed", "invalid snapshot", "probe absent", "offline"} {
		t.Run(name, func(t *testing.T) {
			service, repo, caddy := detailFixture()
			probe := &detailUpstreamProbe{}
			service.upstreamProbe = probe
			hash := repo.latest.Hash
			switch name {
			case "stale":
				hash = "old"
			case "disabled":
				repo.latest.Services[0].Enabled = false
			case "draft only":
				repo.latest.Services = nil
			case "drift":
				caddy.raw = []byte(`{"drift":true}`)
			case "uncertain":
				repo.pending = domain.Deployment{ID: "pending", Status: "uncertain"}
			case "protected":
				repo.latest.Services[0].Host = "127.0.0.1"
				repo.latest.Services[0].Dial = "127.0.0.1:2283"
			case "revoked":
				policy := service.activeSettings()
				policy.DeniedIPs = []string{"10.42.0.8"}
				service.setActivePolicy(policy)
			case "unlicensed":
				repo.latest.Services[0].Host = "unlicensed.internal"
			case "invalid snapshot":
				repo.latest.Services[0].Dial = "photos.internal:2283"
			case "probe absent":
				service.upstreamProbe = nil
			case "offline":
				caddy.err = errors.New("offline")
			}
			if _, err := service.CheckUpstream(context.Background(), "photos", hash, repo.latest.ID); err == nil {
				t.Fatal("check unexpectedly allowed")
			}
			if probe.target != "" {
				t.Fatal("connected before checking security/state")
			}
		})
	}
}
func TestCheckUpstreamInvalidatesResultWhenPublishChanges(t *testing.T) {
	service, repo, _ := detailFixture()
	probe := &detailUpstreamProbe{check: func(context.Context, string) domain.UpstreamCheck {
		repo.latest.ID = "release-2"
		repo.latest.Hash = domain.Fingerprint([]byte(`{"changed":true}`))
		return domain.UpstreamCheck{Status: "reachable"}
	}}
	service.upstreamProbe = probe
	result, err := service.CheckUpstream(context.Background(), "photos", repo.latest.Hash, repo.latest.ID)
	if err != nil || result.Status != "unknown" {
		t.Fatalf("stale result=%+v err=%v", result, err)
	}
}
func TestCheckUpstreamLimitsConcurrencyWithoutHoldingPublishLock(t *testing.T) {
	service, repo, _ := detailFixture()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	service.upstreamProbe = &detailUpstreamProbe{check: func(ctx context.Context, _ string) domain.UpstreamCheck {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("unbounded TCP check")
		}
		close(started)
		<-release
		return domain.UpstreamCheck{Status: "reachable"}
	}}
	go func() {
		_, err := service.CheckUpstream(context.Background(), "photos", repo.latest.Hash, repo.latest.ID)
		done <- err
	}()
	<-started
	_, err := service.CheckUpstream(context.Background(), "photos", repo.latest.Hash, repo.latest.ID)
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.Status != 429 {
		t.Errorf("concurrent check=%v", err)
	}
	if !service.mu.TryLock() {
		t.Error("TCP check held publish lock")
	} else {
		service.mu.Unlock()
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCheckUpstreamInvalidatesSameHashNewDeployment(t *testing.T) {
	service, repo, _ := detailFixture()
	service.upstreamProbe = &detailUpstreamProbe{check: func(context.Context, string) domain.UpstreamCheck {
		repo.latest.ID = "release-2"
		repo.latest.Version = 2
		return domain.UpstreamCheck{Status: "reachable"}
	}}
	result, err := service.CheckUpstream(context.Background(), "photos", repo.latest.Hash, repo.latest.ID)
	if err != nil || result.Status != "unknown" {
		t.Fatalf("same hash new release accepted: result=%+v err=%v", result, err)
	}
}

func TestCheckUpstreamRejectsStaleDeploymentEvenWithSameHash(t *testing.T) {
	service, repo, _ := detailFixture()
	probe := &detailUpstreamProbe{}
	service.upstreamProbe = probe
	repo.latest.ID = "release-2"
	if _, err := service.CheckUpstream(context.Background(), "photos", repo.latest.Hash, "release-1"); err == nil {
		t.Fatal("stale release accepted")
	}
	if probe.target != "" {
		t.Fatal("stale request reached upstream")
	}
}
