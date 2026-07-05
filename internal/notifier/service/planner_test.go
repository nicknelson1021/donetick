package service

import (
	"testing"
	"time"

	chModel "donetick.com/core/internal/chore/model"
	cModel "donetick.com/core/internal/circle/model"
	nModel "donetick.com/core/internal/notifier/model"
)

func TestGenerateNotificationsFromTemplateIncludesRepeatMetadata(t *testing.T) {
	dueDate := time.Now().UTC().Add(24 * time.Hour)
	chore := &chModel.Chore{
		ID:          10,
		Name:        "Take bins out",
		NextDueDate: &dueDate,
		NotificationMetadataV2: &chModel.NotificationMetadata{
			Templates: []*chModel.NotificationTemplate{
				{Value: 1, Unit: chModel.NotificationTemplateUnitDay, Repeat: true},
			},
		},
	}
	assignedUser := &cModel.UserCircleDetail{
		UserCircle: cModel.UserCircle{
			UserID:   20,
			CircleID: 30,
		},
		DisplayName:      "Nick",
		Username:         "nick",
		NotificationType: nModel.NotificationPlatformTelegram,
		TargetID:         "123",
	}

	notifications := generateNotificationsFromTemplate(chore, assignedUser, nil)
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}

	rawEvent := notifications[0].RawEvent
	if rawEvent["repeat"] != true {
		t.Fatalf("expected repeat metadata to be true, got %#v", rawEvent["repeat"])
	}
	if rawEvent["template_value"] != 1 {
		t.Fatalf("expected template_value to be 1, got %#v", rawEvent["template_value"])
	}
	if rawEvent["template_unit"] != chModel.NotificationTemplateUnitDay {
		t.Fatalf("expected template_unit to be d, got %#v", rawEvent["template_unit"])
	}
}

func TestGenerateNotificationsFromTemplateIgnoresRepeatBeforeDue(t *testing.T) {
	dueDate := time.Now().UTC().Add(24 * time.Hour)
	chore := &chModel.Chore{
		ID:          10,
		Name:        "Take bins out",
		NextDueDate: &dueDate,
		NotificationMetadataV2: &chModel.NotificationMetadata{
			Templates: []*chModel.NotificationTemplate{
				{Value: -1, Unit: chModel.NotificationTemplateUnitHour, Repeat: true},
			},
		},
	}
	assignedUser := &cModel.UserCircleDetail{
		UserCircle: cModel.UserCircle{
			UserID:   20,
			CircleID: 30,
		},
		DisplayName:      "Nick",
		Username:         "nick",
		NotificationType: nModel.NotificationPlatformTelegram,
		TargetID:         "123",
	}

	notifications := generateNotificationsFromTemplate(chore, assignedUser, nil)
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].RawEvent["repeat"] != false {
		t.Fatalf("expected before-due repeat metadata to be false, got %#v", notifications[0].RawEvent["repeat"])
	}
}
