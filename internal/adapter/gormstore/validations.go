package gormstore

import (
	"context"
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
		row := validationRow(record)
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
	return validationRecord(row), err
}
