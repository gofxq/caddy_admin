package gormstore

import (
	"github.com/gofxq/caddy_admin/internal/domain"
)

type Service = domain.Service
type Draft = domain.Draft
type Change = domain.Change
type Session = domain.Session
type Deployment = domain.Deployment
type AuditEvent = domain.AuditEvent
type ManagedSettings = domain.ManagedSettings
type CertificateStatus = domain.CertificateStatus
type CertificateMode = domain.CertificateMode
type AppError = domain.AppError

const (
	CertificateModeBootstrapInternal = domain.CertificateModeBootstrapInternal
	CertificateModeCloudflare        = domain.CertificateModeCloudflare
)

func invalid(message string) error  { return domain.Invalid(message) }
func conflict(message string) error { return domain.Conflict(message) }
func notFound(message string) error { return domain.NotFound(message) }
func ID() string                    { return domain.ID() }
func checkUnique(services []Service) error {
	return domain.ValidateUniqueServices(services)
}
