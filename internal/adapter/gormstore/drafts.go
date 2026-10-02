package gormstore

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
)

func (s *Store) Draft(ctx context.Context) (Draft, error) {
	row, err := gorm.G[draftRow](s.db).Where("id = ?", 1).Take(ctx)
	if err != nil {
		return Draft{}, err
	}
	draft := Draft{Revision: row.Revision}
	return draft, json.Unmarshal([]byte(row.Services), &draft.Services)
}

func (s *Store) SaveService(ctx context.Context, revision int64, service Service, remove bool, actor string) (Draft, error) {
	var result Draft
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := gorm.G[draftRow](tx).Where("id = ?", 1).Take(ctx)
		if err != nil {
			return err
		}
		result.Revision = row.Revision
		if result.Revision != revision {
			return conflict("草稿已被修改，请刷新")
		}
		if err = json.Unmarshal([]byte(row.Services), &result.Services); err != nil {
			return err
		}
		found := false
		action := "service.create"
		out := make([]Service, 0, len(result.Services)+1)
		for _, old := range result.Services {
			if old.ID != service.ID {
				out = append(out, old)
				continue
			}
			found = true
			action = "service.update"
			if remove {
				action = "service.delete"
				continue
			}
			if old.Enabled && !service.Enabled {
				action = "service.disable"
			}
			out = append(out, service)
		}
		if remove && !found {
			return &AppError{Status: 404, Code: "not_found", Message: "服务不存在"}
		}
		if !found && !remove {
			out = append(out, service)
		}
		if err = checkUnique(out); err != nil {
			return err
		}
		result.Revision++
		result.Services = out
		encoded := marshal(out)
		update := tx.Model(&draftRow{}).Where("id = ? AND revision = ?", 1, revision).Updates(map[string]any{"revision": result.Revision, "services": encoded})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return conflict("草稿已被修改，请刷新")
		}
		if err = tx.Create(&draftRevisionRow{Revision: result.Revision, Services: encoded}).Error; err != nil {
			return err
		}
		return auditWith(ctx, tx, auditRow{Time: now(), Actor: actor, Action: action, Object: service.ID, Result: "success", Revision: result.Revision})
	})
	return result, err
}

func (s *Store) DraftRevision(ctx context.Context, revision int64) (Draft, error) {
	row, err := gorm.G[draftRevisionRow](s.db).Where("revision = ?", revision).Take(ctx)
	draft := Draft{Revision: revision}
	if isMissing(err) {
		return draft, &AppError{Status: 404, Code: "not_found", Message: "草稿历史版本不存在"}
	}
	if err == nil {
		err = json.Unmarshal([]byte(row.Services), &draft.Services)
	}
	return draft, err
}

func (s *Store) ReplaceDraft(ctx context.Context, revision int64, services []Service, actor string) (Draft, error) {
	var result Draft
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = replaceDraftWith(ctx, tx, revision, services, actor)
		return err
	})
	return result, err
}

func replaceDraftWith(ctx context.Context, tx *gorm.DB, revision int64, services []Service, actor string) (Draft, error) {
	if err := checkUnique(services); err != nil {
		return Draft{}, err
	}
	if services == nil {
		services = []Service{}
	}
	result := Draft{Revision: revision + 1, Services: services}
	encoded := marshal(services)
	update := tx.Model(&draftRow{}).Where("id = ? AND revision = ?", 1, revision).Updates(map[string]any{"revision": result.Revision, "services": encoded})
	if update.Error != nil {
		return Draft{}, update.Error
	}
	if update.RowsAffected != 1 {
		return Draft{}, conflict("草稿已被修改，请重新预览导入")
	}
	if err := tx.Create(&draftRevisionRow{Revision: result.Revision, Services: encoded}).Error; err != nil {
		return Draft{}, err
	}
	if err := auditWith(ctx, tx, auditRow{Time: now(), Actor: actor, Action: "configuration.import", Object: "draft", Result: "success", Revision: result.Revision}); err != nil {
		return Draft{}, err
	}
	return result, nil
}
