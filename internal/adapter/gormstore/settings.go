package gormstore

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) ManagedSettings(ctx context.Context) (ManagedSettings, error) {
	row, err := gorm.G[managedSettingsRow](s.db).Where("id = ?", 1).Take(ctx)
	if err != nil {
		return ManagedSettings{}, err
	}
	var out ManagedSettings
	return out, json.Unmarshal([]byte(row.Value), &out)
}

func (s *Store) CertificateStatus(ctx context.Context) (CertificateStatus, error) {
	row, err := gorm.G[certificateStateRow](s.db).Where("id = ?", 1).Take(ctx)
	if err != nil {
		return CertificateStatus{}, err
	}
	return CertificateStatus{Mode: CertificateMode(row.Mode), ActivationStatus: row.ActivationStatus, PublicStatus: row.PublicStatus, LastErrorClass: row.LastErrorClass, UpdatedAt: row.UpdatedAt, BeforeHash: row.BeforeHash, CandidateHash: row.CandidateHash}, nil
}

func (s *Store) SetCertificateStatus(ctx context.Context, status CertificateStatus) error {
	return setCertificateStatus(ctx, s.db, status)
}

func (s *Store) SetCertificateStatusAudit(ctx context.Context, status CertificateStatus, actor, result string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := setCertificateStatus(ctx, tx, status); err != nil {
			return err
		}
		return auditWith(ctx, tx, auditRow{Time: now(), Actor: actor, Action: "certificate.activation", Object: "cloudflare", Result: result})
	})
}

func setCertificateStatus(ctx context.Context, db *gorm.DB, status CertificateStatus) error {
	if status.Mode != CertificateModeBootstrapInternal && status.Mode != CertificateModeCloudflare {
		return invalid("证书策略无效")
	}
	switch status.ActivationStatus {
	case "idle", "applying", "uncertain", "failed", "success":
	default:
		return invalid("证书启用状态无效")
	}
	switch status.PublicStatus {
	case "unknown", "pending", "ready", "error":
	default:
		return invalid("公网证书状态无效")
	}
	switch status.LastErrorClass {
	case "", "validation", "conflict", "credentials", "unauthorized", "system_resolution", "unavailable":
	default:
		return invalid("证书错误分类无效")
	}
	row := certificateStateRow{ID: 1, Mode: string(status.Mode), ActivationStatus: status.ActivationStatus, PublicStatus: status.PublicStatus, LastErrorClass: status.LastErrorClass, UpdatedAt: now(), BeforeHash: status.BeforeHash, CandidateHash: status.CandidateHash}
	return db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(&row).Error
}

func (s *Store) SaveSettings(ctx context.Context, revision int64, settings ManagedSettings, actor string) (Draft, error) {
	var result Draft
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := gorm.G[draftRow](tx).Where("id = ?", 1).Take(ctx)
		if err != nil {
			return err
		}
		if row.Revision != revision {
			return conflict("草稿已被修改，请刷新")
		}
		result = Draft{Revision: revision + 1, Settings: settings}
		if err = json.Unmarshal([]byte(row.Services), &result.Services); err != nil {
			return err
		}
		encoded := marshal(settings)
		update := tx.Model(&draftRow{}).Where("id = ? AND revision = ?", 1, revision).Updates(map[string]any{"revision": result.Revision, "settings": encoded})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return conflict("草稿已被修改，请刷新")
		}
		if err = tx.Create(&draftRevisionRow{Revision: result.Revision, Services: row.Services, Settings: encoded}).Error; err != nil {
			return err
		}
		return auditWith(ctx, tx, auditRow{Time: now(), Actor: actor, Action: "settings.update", Object: "draft", Result: "success", Revision: result.Revision})
	})
	return result, err
}
