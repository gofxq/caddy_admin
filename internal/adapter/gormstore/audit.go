package gormstore

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func auditWith(ctx context.Context, db *gorm.DB, event auditRow) error {
	return db.WithContext(ctx).Create(&event).Error
}

func (s *Store) Audit(ctx context.Context, actor, action, object, result string, revision, version int64) error {
	return auditWith(ctx, s.db, auditRow{Time: now(), Actor: actor, Action: action, Object: object, Result: result, Revision: revision, Version: version})
}

func (s *Store) Audits(ctx context.Context, offset, limit int) ([]AuditEvent, error) {
	rows, err := gorm.G[auditRow](s.db).Order("id DESC").Offset(offset).Limit(limit).Find(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, AuditEvent{RollbackID: row.RollbackID, ErrorClass: row.ErrorClass, ID: row.ID, Time: row.Time, Actor: row.Actor, Action: row.Action, Object: row.Object, Result: row.Result, Revision: row.Revision, Version: row.Version})
	}
	return out, nil
}

func (s *Store) LoginAllowed(ctx context.Context, key string) bool {
	allowed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		nowUnix := time.Now().Unix()
		if err := tx.Where("reset < ?", nowUnix).Delete(&loginLimitRow{}).Error; err != nil {
			return err
		}
		global := loginLimitRow{Key: "global", Count: 1, Reset: nowUnix + 300}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.Assignments(map[string]any{"count": gorm.Expr("count + 1")})}).Create(&global).Error; err != nil {
			return err
		}
		global, err := gorm.G[loginLimitRow](tx).Where("key = ?", "global").Take(ctx)
		if err != nil {
			return err
		}
		var client loginLimitRow
		err = tx.Where("key = ?", "client:"+key).Take(&client).Error
		if err != nil && !isMissing(err) {
			return err
		}
		allowed = global.Count <= 100 && (isMissing(err) || client.Count < 10)
		return nil
	})
	return err == nil && allowed
}

func (s *Store) RecordLoginFailure(ctx context.Context, key string) error {
	nowUnix := time.Now().Unix()
	row := loginLimitRow{Key: "client:" + key, Count: 1, Reset: nowUnix + 300}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.Assignments(map[string]any{
		"count": gorm.Expr("CASE WHEN reset <= ? THEN 1 ELSE count + 1 END", nowUnix),
		"reset": gorm.Expr("CASE WHEN reset <= ? THEN ? ELSE reset END", nowUnix, nowUnix+300),
	})}).Create(&row).Error
}

func (s *Store) ValidationAudit(ctx context.Context, actor string, revision int64, rollback string, validationErr error) error {
	result, class := "success", ""
	if validationErr != nil {
		result = "failed"
		class = errorClass(validationErr)
	}
	return auditWith(ctx, s.db, auditRow{Time: now(), Actor: actor, Action: "validation", Object: "draft", Result: result, Revision: revision, RollbackID: rollback, ErrorClass: class})
}

func errorClass(err error) string {
	var appErr *AppError
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case "validation", "conflict", "credentials", "unauthorized", "system_resolution":
			return appErr.Code
		}
	}
	return "unavailable"
}
