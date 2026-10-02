package gormstore

import (
	"context"
	"fmt"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) IsInitialized(ctx context.Context) (bool, error) {
	admins, err := gorm.G[adminRow](s.db).Count(ctx, "id")
	if err != nil {
		return false, err
	}
	settings, err := gorm.G[managedSettingsRow](s.db).Count(ctx, "id")
	if err != nil {
		return false, err
	}
	if admins == 0 && settings == 0 {
		return false, nil
	}
	if admins == 1 && settings == 1 {
		return true, nil
	}
	return false, fmt.Errorf("instance data is incomplete; only fresh or fully initialized current instances are supported")
}

func (s *Store) CompleteSetup(ctx context.Context, credentials application.SetupCredentials, settings domain.ManagedSettings, certificate domain.CertificateStatus) error {
	return s.CompleteSetupWithDraft(ctx, credentials, settings, certificate, nil)
}

func (s *Store) CompleteSetupWithDraft(ctx context.Context, credentials application.SetupCredentials, settings domain.ManagedSettings, certificate domain.CertificateStatus, services []Service) error {
	hash, err := passwordHash(credentials.Password)
	if err != nil {
		return err
	}
	if credentials.Username == "" || len(credentials.Username) > 64 {
		return invalid("用户名必须为 1–64 字节")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var admins, settingsCount int64
		if err := tx.Model(&adminRow{}).Count(&admins).Error; err != nil {
			return err
		}
		if err := tx.Model(&managedSettingsRow{}).Count(&settingsCount).Error; err != nil {
			return err
		}
		if admins != 0 || settingsCount != 0 {
			return conflict("系统已经初始化")
		}
		if err := tx.Create(&adminRow{ID: 1, Username: credentials.Username, Password: hash}).Error; err != nil {
			return err
		}
		if err := tx.Create(&managedSettingsRow{ID: 1, Value: marshal(settings)}).Error; err != nil {
			return err
		}
		if len(services) > 0 {
			if _, err := replaceDraftWith(ctx, tx, 0, services, credentials.Username); err != nil {
				return err
			}
		}
		return setCertificateStatus(ctx, tx, certificate)
	})
}
