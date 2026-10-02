package application

import (
	"context"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func (service *Service) Login(ctx context.Context, username, password, clientAddress string) (domain.Session, error) {
	session, loginErr := service.repository.Login(ctx, username, password)
	auditContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	result := "success"
	if loginErr != nil {
		result = "failed"
		if domain.ErrorClass(loginErr) == "credentials" {
			if err := service.repository.RecordLoginFailure(auditContext, clientAddress); err != nil {
				return domain.Session{}, err
			}
		}
	}
	if err := service.repository.Audit(auditContext, "anonymous", "auth.login", "admin", result, 0, 0); err != nil {
		if session.Token != "" {
			_ = service.repository.Logout(auditContext, session.Token)
		}
		return domain.Session{}, err
	}
	return session, loginErr
}

func (service *Service) LoginAllowed(ctx context.Context, clientAddress string) bool {
	return service.repository.LoginAllowed(ctx, clientAddress)
}

func (service *Service) Session(ctx context.Context, token string) (domain.Session, error) {
	return service.repository.Session(ctx, token)
}

func (service *Service) Logout(ctx context.Context, token string) error {
	return service.repository.Logout(ctx, token)
}

func (service *Service) ChangePassword(ctx context.Context, current, password string) error {
	return service.repository.ChangePassword(ctx, current, password)
}

func (service *Service) ResetPassword(ctx context.Context, password string) error {
	return service.repository.ResetPassword(ctx, password)
}
