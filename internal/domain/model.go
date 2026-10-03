package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

type Service struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DomainID  string `json:"domain_id"`
	Hostname  string `json:"hostname"`
	Scheme    string `json:"scheme"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Enabled   bool   `json:"enabled"`
	Notes     string `json:"notes"`
	Dial      string `json:"dial"`
	UpdatedAt string `json:"updated_at"`
}

type Draft struct {
	Settings ManagedSettings `json:"settings"`
	Revision int64           `json:"revision"`
	Services []Service       `json:"services"`
}

type Change struct {
	Kind     string   `json:"kind"`
	Hostname string   `json:"hostname"`
	Before   *Service `json:"before,omitempty"`
	After    *Service `json:"after,omitempty"`
}

type AppError struct {
	Status  int
	Code    string
	Message string
}

func (e *AppError) Error() string { return e.Message }

func Invalid(message string) error {
	return &AppError{Status: 422, Code: "validation", Message: message}
}
func Conflict(message string) error {
	return &AppError{Status: 409, Code: "conflict", Message: message}
}

func NotFound(message string) error {
	return &AppError{Status: 404, Code: "not_found", Message: message}
}

func IsMissing(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && (appErr.Status == 404 || appErr.Code == "not_found")
}

func ErrorClass(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case "validation", "conflict", "credentials", "unauthorized", "system_resolution":
			return appErr.Code
		}
	}
	return "unavailable"
}

func ID() string {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

func Fingerprint(raw []byte) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	canonical, _ := json.Marshal(value)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func Diff(before, after []Service) []Change {
	changes := []Change{}
	previous := map[string]Service{}
	for _, service := range before {
		previous[service.ID] = service
	}
	for _, service := range after {
		old, exists := previous[service.ID]
		if !exists {
			current := service
			changes = append(changes, Change{Kind: "added", Hostname: service.Hostname, After: &current})
		} else {
			comparableOld, comparableNew := old, service
			comparableOld.UpdatedAt, comparableNew.UpdatedAt = "", ""
			if comparableOld != comparableNew {
				kind := "updated"
				if old.Enabled != service.Enabled {
					kind = "disabled"
					if service.Enabled {
						kind = "enabled"
					}
				}
				oldCopy, newCopy := old, service
				changes = append(changes, Change{Kind: kind, Hostname: service.Hostname, Before: &oldCopy, After: &newCopy})
			}
		}
		delete(previous, service.ID)
	}
	for _, service := range previous {
		old := service
		changes = append(changes, Change{Kind: "deleted", Hostname: service.Hostname, Before: &old})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Hostname < changes[j].Hostname })
	return changes
}
