package user

import (
	"testing"
	"time"

	nModel "donetick.com/core/internal/notifier/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestInsertNotificationIsIdempotentForSourceNotification(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&nModel.Notification{}); err != nil {
		t.Fatalf("migrate notifications: %v", err)
	}

	repo := NewNotificationRepository(db)
	sourceID := 42
	newNotification := func() *nModel.Notification {
		return &nModel.Notification{
			ChoreID:              1,
			CircleID:             2,
			UserID:               3,
			TargetID:             "target",
			Text:                 "Reminder",
			ScheduledFor:         time.Now().UTC().Add(time.Hour),
			CreatedAt:            time.Now().UTC(),
			SourceNotificationID: &sourceID,
		}
	}

	if err := repo.InsertNotification(newNotification()); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := repo.InsertNotification(newNotification()); err != nil {
		t.Fatalf("idempotent insert: %v", err)
	}

	var count int64
	if err := db.Model(&nModel.Notification{}).Count(&count).Error; err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if count != 1 {
		t.Fatalf("notification count = %d, want 1", count)
	}
}
