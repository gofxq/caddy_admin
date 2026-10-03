package gormstore

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const currentSchemaVersion = 7

var currentSchemaModels = []any{
	&schemaVersionRow{},
	&draftRow{},
	&adminRow{},
	&sessionRow{},
	&validationRow{},
	&deploymentRow{},
	&auditRow{},
	&loginLimitRow{},
	&draftRevisionRow{},
	&managedSettingsRow{},
	&certificateStateRow{},
}

func initializeOrValidateSchema(db *gorm.DB) error {
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return err
	}
	nonSystem := 0
	for _, table := range tables {
		if !strings.HasPrefix(table, "sqlite_") {
			nonSystem++
		}
	}
	if nonSystem == 0 {
		return initializeV7(db)
	}
	if !db.Migrator().HasTable(&schemaVersionRow{}) {
		return fmt.Errorf("database missing schema version")
	}
	var versions []schemaVersionRow
	if err = db.Find(&versions).Error; err != nil {
		return err
	}
	if len(versions) != 1 || versions[0].Version != currentSchemaVersion {
		return fmt.Errorf("unsupported or malformed database schema version")
	}
	return validateV7(db)
}

func initializeV7(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Migrator().CreateTable(currentSchemaModels...); err != nil {
			return err
		}
		if !tx.Migrator().HasIndex(&deploymentRow{}, "one_pending_deployment") {
			if err := tx.Migrator().CreateIndex(&deploymentRow{}, "one_pending_deployment"); err != nil {
				return err
			}
		}
		seeds := []any{
			&schemaVersionRow{Version: currentSchemaVersion},
			&draftRow{ID: 1, Revision: 0, Services: "[]", Settings: "{}"},
			&draftRevisionRow{Revision: 0, Services: "[]", Settings: "{}"},
			&certificateStateRow{ID: 1, Mode: string(CertificateModeBootstrapInternal), ActivationStatus: "idle", PublicStatus: "unknown", UpdatedAt: now()},
		}
		for _, seed := range seeds {
			if err := tx.Create(seed).Error; err != nil {
				return err
			}
		}
		return validateV7(tx)
	})
}

func validateV7(db *gorm.DB) error {
	for _, model := range currentSchemaModels {
		if !db.Migrator().HasTable(model) {
			return fmt.Errorf("database schema is incomplete")
		}
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		for _, field := range stmt.Schema.Fields {
			if field.IgnoreMigration || field.DBName == "" {
				continue
			}
			if !db.Migrator().HasColumn(model, field.DBName) {
				return fmt.Errorf("database schema is incomplete")
			}
		}
	}
	if !db.Migrator().HasIndex(&deploymentRow{}, "one_pending_deployment") {
		return fmt.Errorf("database schema is incomplete")
	}
	var versions []schemaVersionRow
	if err := db.Find(&versions).Error; err != nil || len(versions) != 1 || versions[0].Version != currentSchemaVersion {
		return fmt.Errorf("unsupported or malformed database schema version")
	}
	var draftCount, certificateCount int64
	if err := db.Model(&draftRow{}).Where("id = ?", 1).Count(&draftCount).Error; err != nil {
		return err
	}
	if err := db.Model(&certificateStateRow{}).Where("id = ?", 1).Count(&certificateCount).Error; err != nil {
		return err
	}
	if draftCount != 1 || certificateCount != 1 {
		return fmt.Errorf("database schema is incomplete")
	}
	return nil
}
