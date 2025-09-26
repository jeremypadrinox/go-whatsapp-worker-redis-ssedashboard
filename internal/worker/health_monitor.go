package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gowhatsapp-worker/internal/config"

	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

type HealthMonitor struct {
	config       *config.Config
	processor    *Processor
	logger       *logrus.Logger
	cron         *cron.Cron
	entryID      cron.EntryID
	alertManager *AlertManager
}

type AlertManager struct {
	logger      *logrus.Logger
	alerts      map[string]*Alert
	lastAlerted map[string]time.Time
}

type Alert struct {
	ID          string
	Type        string
	Severity    string
	Message     string
	TriggeredAt time.Time
	ResolvedAt  *time.Time
}

func NewAlertManager(logger *logrus.Logger) *AlertManager {
	return &AlertManager{
		logger:      logger,
		alerts:      make(map[string]*Alert),
		lastAlerted: make(map[string]time.Time),
	}
}

func (am *AlertManager) CheckQueueBacklogAlert(queueBacklogMinutes float64) {
	alertID := "queue_backlog_high"
	if queueBacklogMinutes > 15.0 {
		am.triggerAlert(alertID, "queue", "warning",
			fmt.Sprintf("Queue backlog is %.1f minutes (threshold: 15 minutes)", queueBacklogMinutes))
	} else {
		am.resolveAlert(alertID)
	}
}

func (am *AlertManager) triggerAlert(alertID, alertType, severity, message string) {
	now := time.Now()

	if lastAlert, exists := am.lastAlerted[alertID]; exists && now.Sub(lastAlert) < 5*time.Minute {
		return
	}

	alert := &Alert{
		ID:          alertID,
		Type:        alertType,
		Severity:    severity,
		Message:     message,
		TriggeredAt: now,
	}

	am.alerts[alertID] = alert
	am.lastAlerted[alertID] = now

	am.logger.WithFields(logrus.Fields{
		"alert_id": alertID,
		"type":     alertType,
		"severity": severity,
		"message":  message,
	}).Warn("Alert triggered")
}

func (am *AlertManager) resolveAlert(alertID string) {
	if alert, exists := am.alerts[alertID]; exists && alert.ResolvedAt == nil {
		now := time.Now()
		alert.ResolvedAt = &now
		am.logger.WithField("alert_id", alertID).Info("Alert resolved")
	}
}

func NewHealthMonitor(cfg *config.Config, processor *Processor, logger *logrus.Logger) *HealthMonitor {
	return &HealthMonitor{
		config:       cfg,
		processor:    processor,
		logger:       logger,
		cron:         cron.New(cron.WithSeconds()),
		alertManager: NewAlertManager(logger),
	}
}

func (hm *HealthMonitor) Start() error {
	hm.entryID, _ = hm.cron.AddFunc("@every "+hm.config.HealthCheckInterval.String(), hm.performHealthCheck)

	hm.cron.Start()

	hm.logger.WithField("interval", hm.config.HealthCheckInterval).Info("Health monitor started")
	return nil
}

func (hm *HealthMonitor) Stop() {
	if hm.cron != nil {
		hm.cron.Stop()
	}
	hm.logger.Info("Health monitor stopped")
}

func (hm *HealthMonitor) performHealthCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := hm.processor.HealthCheck(ctx); err != nil {
		hm.logger.WithError(err).Error("Health check failed")
		return
	}

	if err := hm.processor.RecoverStuckMessages(ctx); err != nil {
		hm.logger.WithError(err).Error("Failed to recover stuck messages")
	}

	if err := hm.reclaimIdleStreamMessages(ctx, 1*time.Hour, 50); err != nil {
		hm.logger.WithError(err).Warn("Stream reclaim encountered an issue")
	}

	hm.logStatistics()

	hm.performAlertingChecks(ctx)
}

func (hm *HealthMonitor) performAlertingChecks(ctx context.Context) {
	if dbStats, err := hm.processor.GetMessageStats(ctx); err == nil {
		pendingCount := dbStats.PendingMessages + dbStats.ProcessingMessages
		if pendingCount > 0 {
			estimatedMinutes := float64(pendingCount) / 10.0
			hm.alertManager.CheckQueueBacklogAlert(estimatedMinutes)
		}
	}
}

func (hm *HealthMonitor) logStatistics() {
	stats := hm.processor.GetStats()

	uptime := time.Since(stats.StartTime)

	var messagesPerMinute float64
	if uptime.Minutes() > 0 {
		messagesPerMinute = float64(stats.MessagesProcessed) / uptime.Minutes()
	}

	var successRate float64
	if stats.MessagesProcessed > 0 {
		successRate = float64(stats.MessagesSent) / float64(stats.MessagesProcessed) * 100
	}

	hm.logger.WithFields(logrus.Fields{
		"uptime_minutes":       uptime.Minutes(),
		"messages_processed":   stats.MessagesProcessed,
		"messages_sent":        stats.MessagesSent,
		"messages_failed":      stats.MessagesFailed,
		"retry_count":          stats.RetryCount,
		"messages_per_minute":  messagesPerMinute,
		"success_rate_percent": successRate,
		"last_message_time":    stats.LastMessageTime,
		"time_since_last_msg":  time.Since(stats.LastMessageTime).String(),
	}).Info("Worker statistics")

	if dbStats, err := hm.processor.GetMessageStats(context.Background()); err == nil {
		hm.logger.WithFields(logrus.Fields{
			"db_total_messages":      dbStats.TotalMessages,
			"db_pending_messages":    dbStats.PendingMessages,
			"db_processing_messages": dbStats.ProcessingMessages,
			"db_sent_messages":       dbStats.SentMessages,
			"db_failed_messages":     dbStats.FailedMessages,
		}).Info("Database statistics")
	}
}

func (hm *HealthMonitor) GetCron() *cron.Cron {
	return hm.cron
}

// reclaimIdleStreamMessages reclaims old pending messages and re-injects them to the queue for processing.
// The processor/DB logic will enforce the max retries (3) and final failure handling.
func (hm *HealthMonitor) reclaimIdleStreamMessages(ctx context.Context, minIdle time.Duration, batch int64) error {
	if hm.processor == nil || hm.processor.redis == nil {
		return nil
	}

	msgs, err := hm.processor.redis.ReclaimIdleMessages(ctx, minIdle, batch)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	reclaimed := 0
	for _, m := range msgs {
		raw, ok := m.Values["data"]
		if !ok {
			_ = hm.processor.redis.AckMessageStream(ctx, m.ID)
			continue
		}
		var payload []byte
		switch v := raw.(type) {
		case string:
			payload = []byte(v)
		case []byte:
			payload = v
		default:
			b, _ := json.Marshal(v)
			payload = b
		}

		if err := hm.processor.redis.PushMessage(ctx, payload); err != nil {
			hm.logger.WithError(err).Warn("Failed to requeue reclaimed message")
			continue
		}

		if err := hm.processor.redis.AckMessageStream(ctx, m.ID); err != nil {
			hm.logger.WithError(err).Warn("Failed to ACK reclaimed message")
			continue
		}
		reclaimed++
	}

	hm.logger.WithFields(logrus.Fields{
		"reclaimed": reclaimed,
		"min_idle":  minIdle.String(),
	}).Info("Reclaimed idle stream messages")

	return nil
}
