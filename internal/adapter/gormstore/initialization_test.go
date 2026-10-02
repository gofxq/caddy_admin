package gormstore

import (
	"context"
	"testing"
)

func TestInitializationRejectsPartialInstanceWithoutWriting(t *testing.T) {
	for _, part := range []string{"administrator", "settings"} {
		t.Run(part, func(t *testing.T) {
			store := testStore(t)
			ctx := context.Background()
			if initialized, err := store.IsInitialized(ctx); err != nil || initialized {
				t.Fatalf("fresh state: %v %v", initialized, err)
			}
			if part == "administrator" {
				if err := store.db.Create(&adminRow{ID: 1, Username: "admin", Password: "preserved"}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := store.db.Create(&managedSettingsRow{ID: 1, Value: marshal(validManagedSettings())}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if initialized, err := store.IsInitialized(ctx); err == nil || initialized {
				t.Fatalf("partial instance treated as fresh/initialized: %v %v", initialized, err)
			}
			var admins, settings int64
			if err := store.db.Model(&adminRow{}).Count(&admins).Error; err != nil {
				t.Fatal(err)
			}
			if err := store.db.Model(&managedSettingsRow{}).Count(&settings).Error; err != nil {
				t.Fatal(err)
			}
			if admins+settings != 1 {
				t.Fatal("partial instance data was modified")
			}
		})
	}
}
