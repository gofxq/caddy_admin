package domain

type CertificateMode string

const (
	CertificateModeBootstrapInternal CertificateMode = "bootstrap_internal"
	CertificateModeCloudflare        CertificateMode = "cloudflare"
)

type ManagedDomain struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Access *string `json:"access"`
}

func DomainAccess(value string) *string { return &value }
func (s ManagedSettings) Domain(id string) (ManagedDomain, bool) {
	for _, d := range s.Domains {
		if d.ID == id {
			return d, true
		}
	}
	return ManagedDomain{}, false
}
func (s ManagedSettings) AdminBase() string {
	for _, d := range s.Domains {
		if OneLevel(s.AdminDomain, d.Name) {
			return d.Name
		}
	}
	return ""
}

type ManagedSettings struct {
	Origin              string          `json:"origin"`
	Domains             []ManagedDomain `json:"domains"`
	ConsoleLANOnly      bool            `json:"console_lan_only"`
	PreviousAdminDomain string          `json:"previous_admin_domain,omitempty"`
	PreviousOrigin      string          `json:"previous_origin,omitempty"`
	AdminDomain         string          `json:"admin_domain"`
	LAN                 []string        `json:"lan_cidrs"`
	UpstreamCIDRs       []string        `json:"upstream_cidrs"`
	AllowedNames        []string        `json:"allowed_names"`
	DeniedIPs           []string        `json:"denied_ips"`
	Resolvers           []string        `json:"resolvers"`
}

type CertificateStatus struct {
	Mode             CertificateMode `json:"mode"`
	ActivationStatus string          `json:"activation_status"`
	PublicStatus     string          `json:"public_status"`
	LastErrorClass   string          `json:"last_error_class"`
	UpdatedAt        string          `json:"updated_at"`
	BeforeHash       string          `json:"-"`
	CandidateHash    string          `json:"-"`
}

type Certificate struct {
	Subject   string   `json:"subject"`
	Status    string   `json:"status"`
	Message   string   `json:"message"`
	SANs      []string `json:"sans"`
	NotBefore string   `json:"not_before"`
	NotAfter  string   `json:"not_after"`
	Days      int      `json:"days"`
	CheckedAt string   `json:"checked_at"`
}

type Overview struct {
	Reachable     bool         `json:"reachable"`
	Drift         bool         `json:"drift"`
	RuntimeHash   string       `json:"runtime_hash"`
	ExpectedHash  string       `json:"expected_hash"`
	Version       int64        `json:"version"`
	Enabled       int          `json:"enabled"`
	DraftRevision int64        `json:"draft_revision"`
	Unpublished   bool         `json:"unpublished"`
	Message       string       `json:"message"`
	Recent        []Deployment `json:"recent"`
	CheckedAt     string       `json:"checked_at"`
}
