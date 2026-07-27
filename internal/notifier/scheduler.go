package notifier

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"donetick.com/core/config"
	chModel "donetick.com/core/internal/chore/model"
	chRepo "donetick.com/core/internal/chore/repo"
	"donetick.com/core/internal/events"
	nModel "donetick.com/core/internal/notifier/model"
	nRepo "donetick.com/core/internal/notifier/repo"
	nService "donetick.com/core/internal/notifier/service"
	uRepo "donetick.com/core/internal/user/repo"
	"donetick.com/core/logging"
)

type keyType string

const (
	SchedulerKey keyType = "scheduler"
)

type Scheduler struct {
	choreRepo        *chRepo.ChoreRepository
	userRepo         *uRepo.UserRepository
	stopChan         chan bool
	notifier         *Notifier
	eventsProducer   *events.EventsProducer
	notificationRepo *nRepo.NotificationRepository
	planner          *nService.NotificationPlanner
	SchedulerJobs    config.SchedulerConfig
}

func NewScheduler(cfg *config.Config, ur *uRepo.UserRepository, cr *chRepo.ChoreRepository, n *Notifier, nr *nRepo.NotificationRepository, ep *events.EventsProducer, planner *nService.NotificationPlanner) *Scheduler {
	return &Scheduler{
		choreRepo:        cr,
		userRepo:         ur,
		stopChan:         make(chan bool),
		notifier:         n,
		notificationRepo: nr,
		eventsProducer:   ep,
		planner:          planner,
		SchedulerJobs:    cfg.SchedulerJobs,
	}
}

func (s *Scheduler) Start(c context.Context) {
	log := logging.FromContext(c)
	log.Debug("Scheduler started")
	if err := s.reconcileOverdueRepeatNotifications(c); err != nil {
		log.Error("Error reconciling overdue repeat notifications", err)
	}
	go s.runScheduler(c, " NOTIFICATION_SCHEDULER ", s.loadAndSendNotificationJob, 3*time.Minute)
	go s.runScheduler(c, " NOTIFICATION_CLEANUP ", s.cleanupSentNotifications, 24*time.Hour*30)
}
func (s *Scheduler) cleanupSentNotifications(c context.Context) (time.Duration, error) {
	log := logging.FromContext(c)
	deleteBefore := time.Now().UTC().Add(-time.Hour * 24 * 30)
	err := s.notificationRepo.DeleteSentNotifications(c, deleteBefore)
	if err != nil {
		log.Error("Error deleting sent notifications", err)
		return time.Duration(0), err
	}
	return time.Duration(0), nil
}

func (s *Scheduler) loadAndSendNotificationJob(c context.Context) (time.Duration, error) {
	log := logging.FromContext(c)
	startTime := time.Now().UTC()
	getAllPendingNotifications, err := s.notificationRepo.GetPendingNotification(c, time.Minute*900)
	log.Debug("Getting pending notifications", " count ", len(getAllPendingNotifications))

	if err != nil {
		log.Error("Error getting pending notifications")
		return time.Since(startTime), err
	}

	sentNotifications := make([]*nModel.NotificationDetails, 0, len(getAllPendingNotifications))
	for _, notification := range getAllPendingNotifications {
		// Persist the next occurrence before delivering the current one. The
		// source-notification unique index makes this safe to retry.
		if err := s.scheduleNextOverdueReminder(c, notification); err != nil {
			log.Error("Error scheduling next overdue reminder", err)
			continue
		}

		err := s.notifier.SendNotification(c, notification)
		if err != nil {
			log.Error("Error sending notification", err)
			continue
		}
		if notification.RawEvent != nil && notification.WebhookURL != nil {
			// if we have a webhook url, we should send the event to the webhook
			s.eventsProducer.NotificationEvent(c, *notification.WebhookURL, notification.RawEvent)
		}

		notification.IsSent = true
		sentNotifications = append(sentNotifications, notification)
	}

	if err := s.notificationRepo.MarkNotificationsAsSent(sentNotifications); err != nil {
		return time.Since(startTime), err
	}
	return time.Since(startTime), nil
}

func (s *Scheduler) scheduleNextOverdueReminder(c context.Context, notification *nModel.NotificationDetails) error {
	if notification.RawEvent == nil || !rawBool(notification.RawEvent, "repeat") {
		return nil
	}
	if rawString(notification.RawEvent, "type") != "overdue" {
		return nil
	}

	value, ok := rawInt(notification.RawEvent, "template_value")
	if !ok || value <= 0 {
		return nil
	}
	unit := chModel.NotificationTemplateUnit(rawString(notification.RawEvent, "template_unit"))
	interval, err := overdueReminderInterval(value, unit)
	if err != nil {
		return err
	}

	chore, err := s.choreRepo.GetChoreByID(c, notification.ChoreID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if !chore.IsActive || !chore.Notification || chore.NextDueDate == nil || !chore.NextDueDate.UTC().Before(now) {
		return nil
	}

	originalDueDate, ok := rawTime(notification.RawEvent, "due_date")
	if ok && !chore.NextDueDate.UTC().Equal(originalDueDate.UTC()) {
		return nil
	}

	nextScheduledFor := nextFutureTime(notification.ScheduledFor.UTC(), interval, now)
	sourceNotificationID := notification.ID
	nextNotification := &nModel.Notification{
		ChoreID:              notification.ChoreID,
		IsSent:               false,
		ScheduledFor:         nextScheduledFor,
		CreatedAt:            now,
		TypeID:               notification.TypeID,
		UserID:               notification.UserID,
		CircleID:             notification.CircleID,
		TargetID:             notification.TargetID,
		Text:                 notification.Text,
		RawEvent:             notification.RawEvent,
		SourceNotificationID: &sourceNotificationID,
	}
	return s.notificationRepo.InsertNotification(nextNotification)
}

func (s *Scheduler) reconcileOverdueRepeatNotifications(c context.Context) error {
	chores, err := s.choreRepo.GetActiveOverdueChores(c, time.Now().UTC())
	if err != nil {
		return err
	}

	for _, chore := range chores {
		if !hasRepeatingOverdueTemplate(chore) {
			continue
		}
		if ok := s.planner.GenerateNotifications(c, chore); !ok {
			logging.FromContext(c).Error("Unable to reconcile overdue repeat notifications", "chore_id", chore.ID)
		}
	}
	return nil
}

func hasRepeatingOverdueTemplate(chore *chModel.Chore) bool {
	if chore == nil || chore.NotificationMetadataV2 == nil {
		return false
	}
	for _, template := range chore.NotificationMetadataV2.Templates {
		if template != nil && template.Repeat && template.Value > 0 {
			return true
		}
	}
	return false
}

func overdueReminderInterval(value int, unit chModel.NotificationTemplateUnit) (time.Duration, error) {
	switch unit {
	case chModel.NotificationTemplateUnitMinute:
		return time.Duration(value) * time.Minute, nil
	case chModel.NotificationTemplateUnitHour:
		return time.Duration(value) * time.Hour, nil
	case chModel.NotificationTemplateUnitDay:
		return time.Duration(value) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported overdue reminder unit: %s", unit)
	}
}

func nextFutureTime(previous time.Time, interval time.Duration, now time.Time) time.Time {
	if interval <= 0 {
		return now
	}
	next := previous.Add(interval)
	if next.After(now) {
		return next
	}
	intervalsBehind := math.Floor(now.Sub(next).Seconds()/interval.Seconds()) + 1
	return next.Add(time.Duration(intervalsBehind) * interval)
}

func rawBool(raw nModel.JSONB, key string) bool {
	value, _ := raw[key].(bool)
	return value
}

func rawString(raw nModel.JSONB, key string) string {
	switch value := raw[key].(type) {
	case string:
		return value
	case chModel.NotificationTemplateUnit:
		return string(value)
	default:
		return ""
	}
}

func rawInt(raw nModel.JSONB, key string) (int, bool) {
	switch value := raw[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func rawTime(raw nModel.JSONB, key string) (time.Time, bool) {
	switch value := raw[key].(type) {
	case time.Time:
		return value, true
	case string:
		t, err := time.Parse(time.RFC3339, value)
		return t, err == nil
	default:
		return time.Time{}, false
	}
}

func (s *Scheduler) runScheduler(c context.Context, jobName string, job func(c context.Context) (time.Duration, error), interval time.Duration) {

	for {
		logging.FromContext(c).Debug("Scheduler running ", jobName, " time", time.Now().UTC().String())

		select {
		case <-s.stopChan:
			log.Println("Scheduler stopped")
			return
		default:
			elapsedTime, err := job(c)
			if err != nil {
				logging.FromContext(c).Error("Error running scheduler job", err)
			}
			logging.FromContext(c).Debug("Scheduler job completed", jobName, " time: ", elapsedTime.String())
		}
		time.Sleep(interval)
	}
}

func (s *Scheduler) Stop() {
	s.stopChan <- true
}
