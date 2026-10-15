package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestPreviewCapturesActualRuntimeWithoutChangingDraftOrSnapshot(t *testing.T) {
	svc, store, caddy, snapshot, _ := policyFixture(t)
	ctx := context.Background()
	original := string(snapshot.raw)
	caddy.raw = []byte(`{"admin":{"listen":"unix//run/caddy/admin.sock"},"storage":{"module":"custom","credential":"test-only-secret"},"apps":{"http":{"servers":{"unexpected":{"listen":[":8443"]}}}}}`)
	draft, _ := store.Draft(ctx)
	started := time.Now().UTC()
	for _, validate := range []bool{false, true} {
		if validate {
			caddy.raw = append([]byte(nil), snapshot.raw...)
		}
		p, err := svc.Preview(ctx, "")
		if validate {
			p, err = svc.Validate(ctx, draft.Revision, "", "admin")
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(p.RuntimeConfig) != string(caddy.raw) || p.RuntimeHash != domain.Fingerprint(p.RuntimeConfig) || p.Drift == validate {
			t.Fatalf("preview did not capture actual drifted config: %+v", p)
		}
		if p.RuntimeReadAt.Before(started) || p.RuntimeReadAt.After(time.Now().UTC()) {
			t.Fatal("missing read time")
		}
		// Domain previews must not accidentally serialize the unredacted runtime.
		encoded, _ := json.Marshal(p)
		if strings.Contains(string(encoded), "test-only-secret") {
			t.Fatal("runtime bypassed presentation redaction")
		}
	}
	after, _ := store.Draft(ctx)
	if after.Revision != draft.Revision || string(snapshot.raw) != original {
		t.Fatal("preview/validation changed persistent or running state")
	}
}
