package notifier

import (
	"testing"
	"time"

	chModel "donetick.com/core/internal/chore/model"
)

func TestOverdueReminderInterval(t *testing.T) {
	tests := []struct {
		name  string
		value int
		unit  chModel.NotificationTemplateUnit
		want  time.Duration
	}{
		{name: "minutes", value: 15, unit: chModel.NotificationTemplateUnitMinute, want: 15 * time.Minute},
		{name: "hours", value: 2, unit: chModel.NotificationTemplateUnitHour, want: 2 * time.Hour},
		{name: "days", value: 3, unit: chModel.NotificationTemplateUnitDay, want: 72 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := overdueReminderInterval(tt.value, tt.unit)
			if err != nil {
				t.Fatalf("overdueReminderInterval returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("overdueReminderInterval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextFutureTimeAdvancesPastMissedIntervals(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	previous := now.Add(-49 * time.Hour)

	got := nextFutureTime(previous, 24*time.Hour, now)
	want := now.Add(23 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("nextFutureTime = %v, want %v", got, want)
	}
}

func TestHasRepeatingOverdueTemplate(t *testing.T) {
	tests := []struct {
		name  string
		chore *chModel.Chore
		want  bool
	}{
		{name: "nil chore", chore: nil, want: false},
		{name: "nil metadata", chore: &chModel.Chore{}, want: false},
		{
			name: "non-repeating overdue template",
			chore: &chModel.Chore{NotificationMetadataV2: &chModel.NotificationMetadata{
				Templates: []*chModel.NotificationTemplate{{Value: 1, Unit: chModel.NotificationTemplateUnitDay}},
			}},
			want: false,
		},
		{
			name: "repeating overdue template",
			chore: &chModel.Chore{NotificationMetadataV2: &chModel.NotificationMetadata{
				Templates: []*chModel.NotificationTemplate{{Value: 2, Unit: chModel.NotificationTemplateUnitHour, Repeat: true}},
			}},
			want: true,
		},
		{
			name: "before-due repeat is ignored",
			chore: &chModel.Chore{NotificationMetadataV2: &chModel.NotificationMetadata{
				Templates: []*chModel.NotificationTemplate{{Value: -2, Unit: chModel.NotificationTemplateUnitHour, Repeat: true}},
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasRepeatingOverdueTemplate(tt.chore); got != tt.want {
				t.Fatalf("hasRepeatingOverdueTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}
