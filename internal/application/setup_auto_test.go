package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type autoSetupRepository struct {
	setupRepository
	initialized        atomic.Bool
	count              atomic.Int32
	credentials        SetupCredentials
	settings           domain.ManagedSettings
	fail               error
	commitDespiteError bool
}

func (r *autoSetupRepository) IsInitialized(context.Context) (bool, error) {
	return r.initialized.Load(), nil
}
func (r *autoSetupRepository) CompleteSetup(_ context.Context, c SetupCredentials, s domain.ManagedSettings, _ domain.CertificateStatus) error {
	r.count.Add(1)
	r.credentials = c
	r.settings = s
	if r.fail == nil || r.commitDespiteError {
		r.initialized.Store(true)
	}
	return r.fail
}
func (*setupCaddy) Validate(context.Context, []byte) error { return nil }
func autoSetupFixture() (*Service, *autoSetupRepository, *setupDNSStub, *crashingSetupSnapshot) {
	r := &autoSetupRepository{}
	dns := &setupDNSStub{plan: SetupDNSPlan{ZoneID: "zone", Name: "*.home.example.com", Type: "A", Address: "10.0.0.6", Action: "reuse", Fingerprint: "same", ContextFingerprint: "context"}}
	snapshot := &crashingSetupSnapshot{}
	s := New(Options{SetupPassword: "secure-password", SetupToken: strings.Repeat("a", 40), SetupDefaults: SetupDefaults{Domain: "*.Home.Example.com.", Username: "admin", Address: "10.0.0.6", Resolvers: []string{"1.1.1.1"}}, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, r, &setupCaddy{}, Dependencies{SetupDoH: autoDoHStub{verified: true}, SetupDNS: dns, SetupIntent: &setupIntentMemory{}, Secrets: &setupSecret{}, Snapshot: snapshot, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: true}})
	return s, r, dns, snapshot
}
func awaitAuto(t *testing.T, s *Service, done func()) SetupProgress {
	t.Helper()
	select {
	case <-s.StartAutomaticSetup(context.Background(), done):
	case <-time.After(3 * time.Second):
		t.Fatal("automatic task did not finish")
	}
	return s.AutomaticSetupProgress()
}
func TestAutomaticSetupReusesDNSAndSavesOnlyConsole(t *testing.T) {
	s, r, dns, snapshot := autoSetupFixture()
	var restarts atomic.Int32
	p := awaitAuto(t, s, func() { restarts.Add(1) })
	if p.Status != "saved" || p.Stage != "restarting" || r.count.Load() != 1 || dns.calls != 0 || restarts.Load() != 1 {
		t.Fatal(p, r.count.Load(), dns.calls, restarts.Load())
	}
	if r.credentials.Username != "admin" || r.credentials.Password != s.options.SetupPassword || r.settings.ConsoleLANOnly || len(r.settings.UpstreamCIDRs) != 0 || r.settings.Domains[0].Access != nil || !strings.Contains(string(snapshot.raw), "caddyadmin.home.example.com") {
		t.Fatal("wrong initial state")
	}
	awaitAuto(t, s, func() { restarts.Add(1) })
	if r.count.Load() != 1 || restarts.Load() != 1 {
		t.Fatal("startup task repeated")
	}
	raw, _ := json.Marshal(p)
	for _, secret := range []string{s.options.SetupPassword, s.options.SetupToken, "1.1.1.1", "10.0.0.6", "zone"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("progress leaked", secret)
		}
	}
}
func TestAutomaticSetupSavesWhenOnlyServerConsoleDNSIsMissing(t *testing.T) {
	s, r, dns, snapshot := autoSetupFixture()
	s.setupProbe = setupProbe{resolver: true}
	var restarts atomic.Int32
	p := awaitAuto(t, s, func() { restarts.Add(1) })
	if p.Status != "saved" || r.count.Load() != 1 || restarts.Load() != 1 || len(snapshot.raw) == 0 || dns.calls != 0 {
		t.Fatal("console DNS hint prevented automatic initialization", p, r.count.Load())
	}
	found := false
	for _, check := range p.Checks {
		if check.ID == "admin_dns" {
			found = check.Status == "info"
		}
	}
	if !found {
		t.Fatal("console DNS hint was lost after saving", p)
	}
}

func TestAutomaticSetupMissingRequiredValuesAlwaysKeepsManualFlow(t *testing.T) {
	for _, field := range []string{"password", "token", "domain", "address", "whitespace-address"} {
		t.Run(field, func(t *testing.T) {
			s, r, dns, _ := autoSetupFixture()
			switch field {
			case "password":
				s.options.SetupPassword = ""
			case "token":
				s.options.SetupToken = ""
			case "domain":
				s.options.SetupDefaults.Domain = ""
			case "address":
				s.options.SetupDefaults.Address = ""
			case "whitespace-address":
				s.options.SetupDefaults.Address = " "
			}
			p := awaitAuto(t, s, nil)
			if p.Status != "manual" || r.count.Load() != 0 || dns.calls != 0 {
				t.Fatal(p)
			}
		})
	}
}
func TestAutomaticSetupInvalidConfigurationNeverCommits(t *testing.T) {
	for _, field := range []string{"password", "token", "domain", "address", "username", "resolvers"} {
		t.Run(field, func(t *testing.T) {
			s, r, _, _ := autoSetupFixture()
			switch field {
			case "password":
				s.options.SetupPassword = "short"
			case "token":
				s.options.SetupToken = "bad"
			case "domain":
				s.options.SetupDefaults.Domain = "bad/domain"
			case "address":
				s.options.SetupDefaults.Address = "0.0.0.0"
			case "username":
				s.options.SetupDefaults.Username = strings.Repeat("u", 65)
			case "resolvers":
				s.options.SetupDefaults.Resolvers = []string{"not-an-ip"}
			}
			p := awaitAuto(t, s, nil)
			if p.Status != "failed" || r.count.Load() != 0 {
				t.Fatal(p)
			}
		})
	}
}
func TestAutomaticSetupRequiresDNSAndWarningConsent(t *testing.T) {
	for _, reason := range []string{"create", "update", "dual-stack", "resolver", "conflict", "offline"} {
		t.Run(reason, func(t *testing.T) {
			s, r, dns, _ := autoSetupFixture()
			switch reason {
			case "create", "update":
				dns.plan.Action = reason
			case "dual-stack":
				dns.plan.Warning = "other address"
			case "resolver":
				s.setupProbe = setupProbe{dns: true}
			case "conflict":
				dns.previewError = domain.Invalid("CNAME conflict")
			case "offline":
				dns.previewError = errors.New(s.options.SetupToken)
			}
			p := awaitAuto(t, s, nil)
			if (p.Status != "action_required" && p.Status != "failed") || r.count.Load() != 0 || dns.calls != 0 {
				t.Fatal(p)
			}
			if strings.Contains(p.Message, s.options.SetupToken) {
				t.Fatal("raw failure exposed")
			}
		})
	}
}
func TestAutomaticSetupIgnoresEnvironmentForExistingAndOtherModes(t *testing.T) {
	for _, mode := range []string{"initialized", "external", "test"} {
		t.Run(mode, func(t *testing.T) {
			s, r, dns, _ := autoSetupFixture()
			switch mode {
			case "initialized":
				r.initialized.Store(true)
				s.options.SetupPassword = "bad"
			case "external":
				s.options.AdminURL = "http://127.0.0.1:2019"
			case "test":
				s.options.TestTLS = true
			}
			p := awaitAuto(t, s, nil)
			if p.Status != "manual" || r.count.Load() != 0 || dns.calls != 0 {
				t.Fatal(p)
			}
		})
	}
}
func TestAutomaticSetupProtectsUnknownSnapshotAndPriorIntent(t *testing.T) {
	for _, kind := range []string{"snapshot", "intent"} {
		t.Run(kind, func(t *testing.T) {
			s, r, _, snapshot := autoSetupFixture()
			if kind == "snapshot" {
				snapshot.raw = []byte(`{"unknown":true}`)
			} else {
				s.setupIntent = &setupIntentMemory{intent: SetupIntent{ConfigurationHash: "pending"}}
			}
			p := awaitAuto(t, s, nil)
			if p.Status != "uncertain" || r.count.Load() != 0 {
				t.Fatal(p)
			}
			if kind == "snapshot" && string(snapshot.raw) != `{"unknown":true}` {
				t.Fatal("unknown data overwritten")
			}
		})
	}
}
func TestAutomaticSetupReconcilesDatabaseErrorWithoutRemovingSnapshot(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "not committed", true: "committed response lost"}[committed], func(t *testing.T) {
			s, r, _, snapshot := autoSetupFixture()
			r.fail = errors.New("database error")
			r.commitDespiteError = committed
			p := awaitAuto(t, s, nil)
			want := "uncertain"
			if committed {
				want = "saved"
			}
			if p.Status != want || len(snapshot.raw) == 0 {
				t.Fatal(p, "removed snapshot")
			}
			if !committed {
				if s.setupIntent.(*setupIntentMemory).intent.SnapshotHash == "" {
					t.Fatal("lost ownership")
				}
			}
		})
	}
}

type blockingAutoDNS struct {
	setupDNSStub
	entered  chan struct{}
	release  chan struct{}
	previews atomic.Int32
	change   bool
}

func (d *blockingAutoDNS) Preview(ctx context.Context, _ string, _ string, _ string) (SetupDNSPlan, error) {
	if d.previews.Add(1) == 1 {
		close(d.entered)
		select {
		case <-d.release:
		case <-ctx.Done():
			return SetupDNSPlan{}, ctx.Err()
		}
	}
	p := d.plan
	if d.change && d.previews.Load() > 1 {
		p.Fingerprint = "changed"
	}
	return p, nil
}
func TestAutomaticSetupSerializesManualWritesAndExposesLiveProgress(t *testing.T) {
	s, r, dns, _ := autoSetupFixture()
	blocking := &blockingAutoDNS{setupDNSStub: *dns, entered: make(chan struct{}), release: make(chan struct{})}
	s.setupDNS = blocking
	task := s.StartAutomaticSetup(context.Background(), nil)
	<-blocking.entered
	progress := s.AutomaticSetupProgress()
	if progress.Status != "running" || progress.Stage != "dns" {
		t.Fatal(progress)
	}
	status, err := s.SetupStatus(context.Background())
	if err != nil || status.Automatic.Status != "running" || status.Defaults != nil {
		t.Fatal(status, err)
	}
	if _, err := s.CompleteSetup(context.Background(), SetupRequest{}); err == nil {
		t.Fatal("manual commit raced automatic initialization")
	}
	if _, err := s.ConfirmSetupDNS(context.Background(), validSetupSettings(), SetupCloudflare{}); err == nil {
		t.Fatal("DNS write raced automatic initialization")
	}
	close(blocking.release)
	<-task
	if r.count.Load() != 1 {
		t.Fatal("wrong administrator count", r.count.Load())
	}
}
func TestAutomaticSetupRechecksDNSBeforeSaving(t *testing.T) {
	s, r, dns, snapshot := autoSetupFixture()
	blocking := &blockingAutoDNS{setupDNSStub: *dns, entered: make(chan struct{}), release: make(chan struct{}), change: true}
	close(blocking.release)
	s.setupDNS = blocking
	p := awaitAuto(t, s, nil)
	if p.Status != "action_required" || r.count.Load() != 0 || len(snapshot.raw) != 0 {
		t.Fatal(p, "changed DNS was accepted")
	}
}
func TestManualSetupCannotInferOrOmitEntranceAddress(t *testing.T) {
	for _, address := range []string{"", " ", "0.0.0.0", "::", "127.0.0.1", "fe80::1", "224.0.0.1"} {
		s, r, _, snapshot := autoSetupFixture()
		_, err := s.CompleteSetup(context.Background(), SetupRequest{Username: "admin", UseConfiguredPassword: true, UseConfiguredToken: true, SetupID: s.setupID, Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Address: address, Fingerprint: "same"}})
		if err == nil || r.count.Load() != 0 || len(snapshot.raw) != 0 {
			t.Fatal("accepted manual address", address, err)
		}
	}
}
func TestSetupDefaultsAreValidOnlyAndExistingInstanceIgnoresInvalidDefaults(t *testing.T) {
	s, r, _, _ := autoSetupFixture()
	s.options.SetupDefaults.Address = "::"
	s.options.SetupDefaults.Resolvers = []string{"bad"}
	status, err := s.SetupStatus(context.Background())
	if err != nil || status.Defaults.Address != "" || status.ConfigurationError == "" || status.Defaults.Domain != "home.example.com" {
		t.Fatal(status, err)
	}
	r.initialized.Store(true)
	status, err = s.SetupStatus(context.Background())
	if err != nil || status.Defaults != nil || status.ConfigurationError != "" {
		t.Fatal("existing instance used first-use configuration", status, err)
	}
}

func TestManualOtherModesCanRetryKnownDatabaseFailure(t *testing.T) {
	for _, mode := range []string{"test", "external"} {
		t.Run(mode, func(t *testing.T) {
			s, r, _, snapshot := autoSetupFixture()
			r.fail = errors.New("not committed")
			if mode == "test" {
				s.options.TestTLS = true
			} else {
				s.options.AdminURL = "http://external:2019"
			}
			req := SetupRequest{Username: "admin", UseConfiguredPassword: true, SetupID: s.setupID, Settings: validSetupSettings()}
			s.setupProbe = setupProbe{resolver: true, dns: true, console: true}
			if _, err := s.CompleteSetup(context.Background(), req); err == nil || len(snapshot.raw) != 0 {
				t.Fatal("known failed own snapshot prevents recovery", err)
			}
			r.fail = nil
			if _, err := s.CompleteSetup(context.Background(), req); err != nil || !r.initialized.Load() {
				t.Fatal("cannot retry after storage repaired", err)
			}
		})
	}
}
func TestManualSetupRequiresConfirmationOfReusedDNSWarnings(t *testing.T) {
	s, r, dns, _ := autoSetupFixture()
	dns.plan.Warning = "another address family"
	req := SetupRequest{Username: "admin", UseConfiguredPassword: true, UseConfiguredToken: true, SetupID: s.setupID, Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Address: "10.0.0.6", Fingerprint: "same"}}
	if _, err := s.CompleteSetup(context.Background(), req); err == nil || r.count.Load() != 0 {
		t.Fatal("DNS warning was silently accepted", err)
	}
	req.Cloudflare.Confirmed = true
	if _, err := s.CompleteSetup(context.Background(), req); err != nil || r.count.Load() != 1 {
		t.Fatal("current warning confirmation was not accepted", err)
	}
}

func TestAutomaticSetupPublishesSpecificSanitizedPreflightChecks(t *testing.T) {
	s, r, _, _ := autoSetupFixture()
	s.setupProbe = setupProbe{}
	progress := awaitAuto(t, s, nil)
	if progress.Stage != "preflight" || progress.Status != "action_required" || len(progress.Checks) != 4 || r.count.Load() != 0 {
		t.Fatal("missing preflight details", progress)
	}
	expected := map[string]string{"setup_doh": "pass", "settings_valid": "pass", "resolver_reachable": "warning", "admin_dns": "info"}
	for _, check := range progress.Checks {
		if check.Status != expected[check.ID] || check.Message == "" || check.Status != "pass" && check.NextStep == "" {
			t.Fatal(check)
		}
		if check.ID == "admin_dns" && strings.Contains(check.Message, "浏览器") {
			t.Fatal("automatic server DoH was described as a browser query", check)
		}
	}
	// A reader cannot mutate the cached result shown to another browser.
	progress.Checks[0].Message = "changed"
	if s.AutomaticSetupProgress().Checks[0].Message == "changed" {
		t.Fatal("progress slice is shared with caller")
	}
	raw, _ := json.Marshal(s.AutomaticSetupProgress())
	for _, value := range []string{s.options.SetupPassword, s.options.SetupToken, "1.1.1.1", "10.0.0.6", "home.example.com"} {
		if strings.Contains(string(raw), value) {
			t.Fatal("details leaked deployment value")
		}
	}
}
func TestAutomaticSetupCheckDetailsDoNotForwardRawErrors(t *testing.T) {
	s, _, _, _ := autoSetupFixture()
	s.setSetupProgress("action_required", "preflight", "", SetupCheck{ID: "settings_valid", Status: "block", Message: s.options.SetupToken}, SetupCheck{ID: "unknown", Status: "block", Message: s.options.SetupPassword})
	progress := s.AutomaticSetupProgress()
	raw, _ := json.Marshal(progress)
	if len(progress.Checks) != 1 || strings.Contains(string(raw), s.options.SetupToken) || strings.Contains(string(raw), s.options.SetupPassword) {
		t.Fatal("raw check details exposed")
	}
}

type autoDoHStub struct {
	verified bool
	check    func()
}

func (d autoDoHStub) Check(context.Context, string, string) (SetupDNSReport, error) {
	if d.check != nil {
		d.check()
	}
	return SetupDNSReport{Verified: d.verified}, nil
}
func TestAutomaticSetupChecksDoHBeforeCloudflareAndPausesOnMismatch(t *testing.T) {
	s, r, _, snapshot := autoSetupFixture()
	var order []string
	s.setupDoH = autoDoHStub{verified: false, check: func() { order = append(order, "doh") }}
	s.setupDNS = &orderedSetupDNS{callback: func() { order = append(order, "cloudflare") }}
	p := awaitAuto(t, s, nil)
	if len(order) != 2 || order[0] != "doh" || order[1] != "cloudflare" || p.Status != "action_required" || p.Stage != "doh" || r.count.Load() != 0 || len(snapshot.raw) != 0 {
		t.Fatal(order, p)
	}
}

type orderedSetupDNS struct{ callback func() }

func (d *orderedSetupDNS) Preview(context.Context, string, string, string) (SetupDNSPlan, error) {
	d.callback()
	return SetupDNSPlan{Action: "reuse", Fingerprint: "same"}, nil
}
func (*orderedSetupDNS) Apply(context.Context, string, SetupDNSPlan) error {
	panic("readonly check attempted write")
}
func TestAutomaticSetupRecheckUpdatesResultsWithoutSaving(t *testing.T) {
	s, r, dns, snapshot := autoSetupFixture()
	s.setupDoH = autoDoHStub{verified: false}
	p := awaitAuto(t, s, nil)
	if !p.CanRecheck {
		t.Fatal("mismatch cannot be rechecked")
	}
	s.setupDoH = autoDoHStub{verified: true}
	p, err := s.RecheckAutomaticSetup(context.Background(), s.setupID)
	if err != nil || p.Status != "action_required" || p.Stage != "preflight" || len(p.Checks) != 4 || !p.CanRecheck || r.count.Load() != 0 || dns.calls != 0 || len(snapshot.raw) != 0 {
		t.Fatal(p, err)
	}
	for _, check := range p.Checks {
		if check.Status != "pass" {
			t.Fatal(check)
		}
	}
}
func TestAutomaticSetupRecheckAcceptsConsoleDNSHintWithoutSaving(t *testing.T) {
	s, r, dns, snapshot := autoSetupFixture()
	s.setupProbe = setupProbe{}
	awaitAuto(t, s, nil)
	s.setupProbe = setupProbe{resolver: true}
	p, err := s.RecheckAutomaticSetup(context.Background(), s.setupID)
	if err != nil || p.Status != "action_required" || !strings.Contains(p.Message, "已满足初始化条件") || r.count.Load() != 0 || len(snapshot.raw) != 0 || dns.calls != 0 {
		t.Fatal("recheck rejected a hint or saved initialization", p, err)
	}
	for _, check := range p.Checks {
		if check.Status != "pass" && check.Status != "info" {
			t.Fatal("unexpected warning after resolver recovered", check)
		}
	}
}

func TestAutomaticSetupRecheckRejectsStaleUnsafeAndConcurrentRequests(t *testing.T) {
	for _, state := range []string{"stale", "running", "uncertain", "initialized", "manual"} {
		t.Run(state, func(t *testing.T) {
			s, r, _, _ := autoSetupFixture()
			s.setupDoH = autoDoHStub{verified: false}
			awaitAuto(t, s, nil)
			id := s.setupID
			switch state {
			case "stale":
				id = "stale"
			case "running":
				s.setupMu.Lock()
				defer s.setupMu.Unlock()
			case "uncertain":
				s.setSetupProgress("uncertain", "saving", "")
			case "initialized":
				r.initialized.Store(true)
			case "manual":
				s.setSetupProgress("manual", "configuration", "")
			}
			if _, err := s.RecheckAutomaticSetup(context.Background(), id); err == nil {
				t.Fatal("unsafe retry accepted")
			}
		})
	}
}

type cancellationAwareSetupRepository struct{ *autoSetupRepository }

func (r *cancellationAwareSetupRepository) IsInitialized(ctx context.Context) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return r.autoSetupRepository.IsInitialized(ctx)
}
func TestInterruptedReadonlyRecheckDoesNotCreateUncertainSubmission(t *testing.T) {
	s, r, _, _ := autoSetupFixture()
	s.setupDoH = autoDoHStub{verified: false}
	awaitAuto(t, s, nil)
	s.repository = &cancellationAwareSetupRepository{r}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.RecheckAutomaticSetup(ctx, s.setupID); err == nil {
		t.Fatal("canceled request succeeded")
	}
	if !s.AutomaticSetupProgress().CanRecheck || s.AutomaticSetupProgress().Status == "uncertain" {
		t.Fatal("readonly cancellation was treated as uncertain commit")
	}
	if _, err := s.RecheckAutomaticSetup(context.Background(), s.setupID); err != nil {
		t.Fatal("cannot retry after readonly disconnect", err)
	}
}
