package gormstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gofxq/caddy_admin/internal/application"
	"gorm.io/gorm"
)

type validationRecord = application.ValidationRecord

func (s *Store) SaveValidation(ctx context.Context, record validationRecord, actor string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		draft, err := gorm.G[draftRow](tx).Select("revision").Where("id = ?", 1).Take(ctx)
		if err != nil {
			return err
		}
		if draft.Revision != record.Revision {
			return conflict("校验期间草稿变化，请重试")
		}
		if err := tx.Where("created < ?", time.Now().Add(-time.Hour).Unix()).Delete(&validationRow{}).Error; err != nil {
			return err
		}
		row := validationRow{ID: record.ID, Revision: record.Revision, Config: record.Config, Services: record.Services, Settings: marshal(record.Settings), BaseHash: record.BaseHash, PolicyHash: record.PolicyHash, RollbackID: record.RollbackID, Created: record.Created}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return auditWith(ctx, tx, auditRow{Time: now(), Actor: actor, Action: "validation", Object: "draft", Result: "success", Revision: record.Revision, RollbackID: record.RollbackID})
	})
}

func (s *Store) Validation(ctx context.Context, id string) (validationRecord, error) {
	row, err := gorm.G[validationRow](s.db).Where("id = ?", id).Take(ctx)
	if isMissing(err) {
		return validationRecord{}, notFound("校验记录不存在")
	}
	out := validationRecord{ID: row.ID, Revision: row.Revision, Config: row.Config, Services: row.Services, BaseHash: row.BaseHash, PolicyHash: row.PolicyHash, RollbackID: row.RollbackID, Created: row.Created}
	if err == nil {
		err = json.Unmarshal([]byte(row.Settings), &out.Settings)
	}
	return out, err
}
