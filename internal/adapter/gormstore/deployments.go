package gormstore

import (
	"context"
	"encoding/json"
	"log/slog"

	"gorm.io/gorm"
)

func deploymentFromRow(row deploymentRow) (Deployment, error) {
	d := Deployment{
		ID: row.ID, Version: row.Version, Revision: row.Revision, Status: row.Status,
		Config: row.Config, BaseHash: row.BaseHash, Hash: row.Hash, Actor: row.Actor,
		Created: row.Created, Finished: row.Finished, Error: row.Error,
		RollbackID: row.RollbackID, Idempotency: row.Idempotency, RequestHash: row.RequestHash,
	}
	if err := json.Unmarshal([]byte(row.Settings), &d.Settings); err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(row.Services), &d.Services); err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(row.Changes), &d.Changes); err != nil {
		return d, err
	}
	return d, nil
}

func deploymentToRow(d Deployment) deploymentRow {
	return deploymentRow{
		ID: d.ID, Version: d.Version, Revision: d.Revision, Status: d.Status,
		Settings: marshal(d.Settings), Config: d.Config, Services: marshal(d.Services), BaseHash: d.BaseHash, Hash: d.Hash,
		Actor: d.Actor, Created: d.Created, Finished: d.Finished, Error: d.Error,
		RollbackID: d.RollbackID, Changes: marshal(d.Changes), Idempotency: d.Idempotency,
		RequestHash: d.RequestHash,
	}
}

func (s *Store) Deployment(ctx context.Context, id string) (Deployment, error) {
	row, err := gorm.G[deploymentRow](s.db).Where("id = ?", id).Take(ctx)
	if isMissing(err) {
		return Deployment{}, &AppError{Status: 404, Code: "not_found", Message: "发布记录不存在"}
	}
	if err != nil {
		return Deployment{}, err
	}
	return deploymentFromRow(row)
}

func (s *Store) DeploymentByIdempotency(ctx context.Context, key string) (Deployment, error) {
	row, err := gorm.G[deploymentRow](s.db).Where("idempotency = ?", key).Take(ctx)
	if isMissing(err) {
		return Deployment{}, notFound("发布记录不存在")
	}
	if err != nil {
		return Deployment{}, err
	}
	return deploymentFromRow(row)
}

func (s *Store) Latest(ctx context.Context) (Deployment, error) {
	row, err := gorm.G[deploymentRow](s.db).Where("status = ?", "success").Order("version DESC").Take(ctx)
	if isMissing(err) {
		return Deployment{}, notFound("发布记录不存在")
	}
	if err != nil {
		return Deployment{}, err
	}
	return deploymentFromRow(row)
}

func (s *Store) Pending(ctx context.Context) (Deployment, error) {
	row, err := gorm.G[deploymentRow](s.db).Where("status IN ?", []string{"applying", "uncertain"}).Take(ctx)
	if isMissing(err) {
		return Deployment{}, notFound("发布记录不存在")
	}
	if err != nil {
		return Deployment{}, err
	}
	return deploymentFromRow(row)
}

func (s *Store) Deployments(ctx context.Context, offset, limit int) ([]Deployment, error) {
	rows, err := gorm.G[deploymentRow](s.db).Order("version DESC").Offset(offset).Limit(limit).Find(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Deployment, 0, len(rows))
	for _, row := range rows {
		d, err := deploymentFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *Store) BeginDeployment(ctx context.Context, expectedRevision int64, d Deployment) (Deployment, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		draft, err := gorm.G[draftRow](tx).Select("revision").Where("id = ?", 1).Take(ctx)
		if err != nil {
			return err
		}
		if draft.Revision != expectedRevision {
			return conflict("草稿已变化，请重新校验")
		}
		latest, err := gorm.G[deploymentRow](tx).Select("version").Order("version DESC").Take(ctx)
		if err != nil && !isMissing(err) {
			return err
		}
		d.Version = latest.Version + 1
		row := deploymentToRow(d)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return auditWith(ctx, tx, auditRow{Time: now(), Actor: d.Actor, Action: "deployment.begin", Object: d.ID, Result: "accepted", Revision: d.Revision, Version: d.Version})
	})
	return d, err
}

func (s *Store) finish(ctx context.Context, d Deployment, status, message string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stored, err := gorm.G[deploymentRow](tx).Where("id = ?", d.ID).Take(ctx)
		if err != nil {
			return err
		}
		if stored.Status == status && stored.Error == message {
			return nil
		}
		if status == "success" {
			var committed ManagedSettings
			if err = json.Unmarshal([]byte(stored.Settings), &committed); err != nil {
				return err
			}
			old, oldErr := gorm.G[managedSettingsRow](tx).Where("id = ?", 1).Take(ctx)
			if oldErr != nil && !isMissing(oldErr) {
				return oldErr
			}
			if oldErr == nil {
				var previous ManagedSettings
				if err = json.Unmarshal([]byte(old.Value), &previous); err != nil {
					return err
				}
				if previous.PreviousAdminDomain != "" && committed.PreviousAdminDomain == "" {
					if err = tx.Where("1 = 1").Delete(&sessionRow{}).Error; err != nil {
						return err
					}
				}
				if err = tx.Model(&managedSettingsRow{}).Where("id = ?", 1).Update("value", stored.Settings).Error; err != nil {
					return err
				}
			}
		}
		finished := ""
		if status == "success" || status == "failed" {
			finished = now()
		}
		if err := tx.Model(&deploymentRow{}).Where("id = ?", d.ID).Updates(map[string]any{"status": status, "error": message, "finished": finished}).Error; err != nil {
			return err
		}
		action := "deployment." + status
		if d.RollbackID != "" {
			action = "rollback." + status
		}
		return auditWith(ctx, tx, auditRow{Time: now(), Actor: d.Actor, Action: action, Object: d.ID, Result: message, Revision: d.Revision, Version: d.Version})
	})
	if err != nil {
		slog.Error("deployment_store_failed", "deployment_id", d.ID, "revision", d.Revision, "stage", status, "error_class", "database")
		return err
	}
	class := ""
	if status != "success" {
		class = "deployment_" + status
	}
	slog.Info("deployment_transition", "deployment_id", d.ID, "revision", d.Revision, "stage", status, "error_class", class)
	return nil
}

func (s *Store) FinishDeployment(ctx context.Context, deployment Deployment, status, message string) error {
	return s.finish(ctx, deployment, status, message)
}
