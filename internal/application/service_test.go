package application_test

import (
	"context"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type applicationRepository struct {
	application.Repository
	loginErr       error
	auditContextOK bool
	loginFailure   bool
}

func (repository *applicationRepository) Login(context.Context, string, string) (domain.Session, error) {
	return domain.Session{}, repository.loginErr
}

func (repository *applicationRepository) RecordLoginFailure(context.Context, string) error {
	repository.loginFailure = true
	return nil
}

func (repository *applicationRepository) Audit(ctx context.Context, _, action, _, result string, _, _ int64) error {
	repository.auditContextOK = ctx.Err() == nil && action == "auth.login" && result == "failed"
	return nil
}

type applicationCaddy struct{ application.CaddyPort }

func TestCanceledLoginStillRecordsFailureAndAudit(t *testing.T) {
	repository := &applicationRepository{loginErr: &domain.AppError{Status: 401, Code: "credentials", Message: "用户名或密码错误"}}
	service := application.New(application.Options{}, repository, &applicationCaddy{}, application.Dependencies{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Login(ctx, "admin", "wrong", "192.0.2.1"); err == nil {
		t.Fatal("invalid login unexpectedly succeeded")
	}
	if !repository.loginFailure || !repository.auditContextOK {
		t.Fatalf("detached outcome not persisted: failure=%v audit=%v", repository.loginFailure, repository.auditContextOK)
	}
}
