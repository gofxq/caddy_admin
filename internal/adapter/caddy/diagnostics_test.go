package caddy

import (
	"strings"
	"testing"
)

func TestCAAClassificationAndPrivacy(t *testing.T) {
	cases := []struct {
		name    string
		records []caaRecord
		want    string
	}{
		{"none", nil, "pass"},
		{"allowed", []caaRecord{{Tag: "issue", Value: "letsencrypt.org"}}, "pass"},
		{"wild overrides", []caaRecord{{Tag: "issue", Value: "letsencrypt.org"}, {Tag: "issuewild", Value: ";"}}, "unknown"},
		{"wild denies", []caaRecord{{Tag: "issue", Value: "letsencrypt.org"}, {Tag: "issuewild", Value: "sectigo.com"}}, "fail"},
		{"account restriction", []caaRecord{{Tag: "issue", Value: "letsencrypt.org; accounturi=https://secret.invalid/token"}}, "unknown"},
		{"critical unknown", []caaRecord{{Flags: 128, Tag: "secret", Value: "secret"}}, "fail"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state, message, values := checkCAA(c.records)
			if state != c.want {
				t.Fatal(state, message)
			}
			if strings.Contains(message+strings.Join(values, ","), "secret") {
				t.Fatal("raw CAA leaked")
			}
		})
	}
}
