package domain

// PortalService is the complete anonymous projection; never embed Service here.
type PortalService struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	URL      string `json:"url"`
}
