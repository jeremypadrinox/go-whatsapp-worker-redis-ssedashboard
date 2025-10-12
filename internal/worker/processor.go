package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"gowhatsapp-worker/internal/config"
	"gowhatsapp-worker/internal/database"
	"gowhatsapp-worker/internal/models"
	"gowhatsapp-worker/internal/redis"
	"gowhatsapp-worker/internal/whatsapp"
)

type Processor struct {
	config      *config.Config
	db          database.DatabaseInterface
	redis       *redis.Client
	whatsapp    whatsapp.MessageSender
	rateLimiter *RateLimiter
	logger      *logrus.Logger
	stats       *ProcessingStats
	poolSize    int
}

type ProcessingStats struct {
	MessagesProcessed      int64
	MessagesSent           int64
	MessagesFailed         int64
	RetryCount             int64
	StartTime              time.Time
	LastMessageTime        time.Time
	LastPendingMessageTime time.Time
	QueueDepth             int64
	ActiveWorkers          int32
	Rejections             int64
}

type RedisMessage struct {
	ID             string    `json:"id"`
	Phone          string    `json:"phone,omitempty"`
	Message        string    `json:"message,omitempty"`
	MessageType    string    `json:"message_type"`
	Caption        string    `json:"caption,omitempty"`
	ViewOnce       bool      `json:"view_once,omitempty"`
	ImageURL       string    `json:"image_url,omitempty"`
	FilePath       string    `json:"file_path,omitempty"`
	Compress       bool      `json:"compress,omitempty"`
	Duration       int       `json:"duration,omitempty"`
	IsForwarded    bool      `json:"is_forwarded,omitempty"`
	ReplyMessageID string    `json:"reply_message_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	RetryCount     int       `json:"retry_count"`
	Attempts       int       `json:"attempts,omitempty"`
	MaxAttempts    int       `json:"max_attempts,omitempty"`
}

func NewProcessor(cfg *config.Config, db database.DatabaseInterface, redisClient *redis.Client, wa whatsapp.MessageSender, logger *logrus.Logger) *Processor {
	poolSize := 1
	if cfg.RateProfile == "bulk" {
		poolSize = cfg.WorkerConcurrency
		if poolSize <= 0 {
			poolSize = 5
		}
	}

	return &Processor{
		config:      cfg,
		db:          db,
		redis:       redisClient,
		whatsapp:    wa,
		rateLimiter: NewRateLimiter(cfg),
		logger:      logger,
		stats: &ProcessingStats{
			StartTime:              time.Now(),
			LastPendingMessageTime: time.Now(),
		},
		poolSize: poolSize,
	}
}

func (p *Processor) ProcessMessages(ctx context.Context) error {
	p.logger.WithField("pool_size", p.poolSize).Info("Starting Redis-based message processing with worker pool")

	atomic.StoreInt32(&p.stats.ActiveWorkers, int32(p.poolSize))

	var wg sync.WaitGroup
	for i := 0; i < p.poolSize; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			p.workerLoop(ctx, workerID)
		}(i)
	}

	go p.metricsUpdater(ctx)

	wg.Wait()
	p.logger.Info("All workers stopped")
	return nil
}

func (p *Processor) workerLoop(ctx context.Context, workerID int) {
	logger := p.logger.WithField("worker_id", workerID)
	logger.Info("Worker started")

	messageCount := 0
	lastStatusLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Worker stopped")
			return
		default:
			if err := p.processNextRedisMessage(ctx); err != nil {
				logger.WithError(err).Error("Failed to process Redis message")
			}

			messageCount++
			atomic.AddInt64(&p.stats.MessagesProcessed, 1)

			if time.Since(lastStatusLog) >= 15*time.Minute {
				logger.WithFields(logrus.Fields{
					"total_processed": atomic.LoadInt64(&p.stats.MessagesProcessed),
					"messages_sent":   atomic.LoadInt64(&p.stats.MessagesSent),
					"messages_failed": atomic.LoadInt64(&p.stats.MessagesFailed),
					"retry_count":     atomic.LoadInt64(&p.stats.RetryCount),
				}).Info("Worker status summary")
				lastStatusLog = time.Now()
			}

			rateLimiter := NewRateLimiter(p.config)
			rateLimiter.WaitForNextMessage(messageCount)
		}
	}
}

func shortenID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ifZeroThen(v int, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func (p *Processor) processNextRedisMessage(ctx context.Context) error {
	messageData, msgID, err := p.redis.PopMessageWithID(ctx, 5*time.Second)
	if err != nil {
		if err == redisv8.Nil {
			return nil
		}
		return fmt.Errorf("failed to get message from Redis: %w", err)
	}

	var redisMsg RedisMessage
	if err := json.Unmarshal(messageData, &redisMsg); err != nil {
		return fmt.Errorf("failed to parse Redis message: %w", err)
	}

	if redisMsg.CreatedAt.IsZero() {
		var aux map[string]interface{}
		if err := json.Unmarshal(messageData, &aux); err == nil {
			if ts, ok := aux["created_at"].(string); ok {
				if t, perr := time.Parse(time.RFC3339, ts); perr == nil {
					redisMsg.CreatedAt = t
				}
			}
		}
	}

	p.logger.WithFields(logrus.Fields{
		"message_id":   shortenID(redisMsg.ID),
		"target":       redisMsg.Phone,
		"message_type": redisMsg.MessageType,
	}).Debug("Processing Redis message")

	message := &models.Message{
		ID:             redisMsg.ID,
		Phone:          redisMsg.Phone,
		Message:        redisMsg.Message,
		MessageType:    redisMsg.MessageType,
		Caption:        redisMsg.Caption,
		FilePath:       redisMsg.FilePath,
		ImageURL:       redisMsg.ImageURL,
		ViewOnce:       redisMsg.ViewOnce,
		Compress:       redisMsg.Compress,
		IsForwarded:    redisMsg.IsForwarded,
		ReplyMessageID: redisMsg.ReplyMessageID,
		Duration:       redisMsg.Duration,
		Provider:       "go-whatsapp",
		QueueStatus:    string(models.QueueStatusProcessing),
		Status:         string(models.StatusProcessing),
		Attempts:       maxInt(redisMsg.Attempts, redisMsg.RetryCount),
		MaxAttempts:    ifZeroThen(redisMsg.MaxAttempts, 3),
		CreatedAt:      redisMsg.CreatedAt,
		UpdatedAt:      time.Now(),
	}

	if err := p.db.UpdateMessageStatus(ctx, message.ID, models.StatusProcessing, map[string]interface{}{}); err != nil {
		p.logger.WithError(err).WithField("message_id", shortenID(message.ID)).Error("Failed to update message status to processing")
	}

	success, err := p.sendMessage(ctx, message, &redisMsg)
	if err != nil {
		p.logger.WithError(err).WithField("message_id", shortenID(message.ID)).Error("WhatsApp API call failed")
	}

	finalStatus, err2 := p.updateMessageResult(ctx, message, success, err, &redisMsg)
	if err2 != nil {
		p.logger.WithError(err2).WithField("message_id", shortenID(message.ID)).Error("Failed to update final message status")
		return fmt.Errorf("failed to update message result: %w", err2)
	}

	if success {
		atomic.AddInt64(&p.stats.MessagesSent, 1)
	} else {
		atomic.AddInt64(&p.stats.MessagesFailed, 1)
	}
	p.stats.LastMessageTime = time.Now()

	if success {
		p.logger.WithFields(logrus.Fields{
			"message_id": shortenID(message.ID),
			"target":     message.Phone,
		}).Info("Message sent successfully")
	} else {
		p.logger.WithFields(logrus.Fields{
			"message_id": shortenID(message.ID),
			"target":     message.Phone,
			"error":      err.Error(),
		}).Warn("Message failed to send")
	}

	if msgID != "" {
		if success || finalStatus == models.StatusFailed {
			if ackErr := p.redis.AckMessageStream(ctx, msgID); ackErr != nil {
				p.logger.WithError(ackErr).WithField("message_id", shortenID(message.ID)).Warn("Failed to ACK stream message")
			}
		}
	}

	return nil
}

func (p *Processor) sendMessage(ctx context.Context, message *models.Message, redisMsg *RedisMessage) (bool, error) {
	typingDelay := p.rateLimiter.GetTypingDelay()

	err := p.whatsapp.SendTypingPresence(ctx, message.Phone, true)
	if err != nil {
	}

	time.Sleep(typingDelay)

	messageType := "text"

	if message.APIResponse != nil {
		if respData, ok := message.APIResponse.(map[string]interface{}); ok {
			if msgType, exists := respData["message_type"]; exists {
				messageType = fmt.Sprintf("%v", msgType)
			}
		}
	}

	if messageType == "text" && redisMsg.MessageType != "" {
		messageType = redisMsg.MessageType
	}

	p.logger.WithFields(logrus.Fields{
		"message_id":     shortenID(message.ID),
		"message_type":   messageType,
		"target":         message.Phone,
		"api_response":   message.APIResponse,
		"redis_msg_type": redisMsg.MessageType,
	}).Debug("Processing message with type")

	var resp *models.WhatsAppResponse
	var sendErr error
	var filePath string
	var imageURL string

	p.logger.WithFields(logrus.Fields{
		"message_id":   shortenID(message.ID),
		"message_type": messageType,
		"endpoint":     "/send/" + messageType,
	}).Debug("Routing message to endpoint")

	switch messageType {
	case "image":
		caption := message.Message
		viewOnce := message.ViewOnce
		compress := message.Compress
		filePath = message.FilePath
		imageURL = message.ImageURL

		resp, sendErr = p.whatsapp.SendImage(ctx, message.Phone, caption, filePath, viewOnce, compress, imageURL)

	case "file":
		caption := message.Message
		filePath = message.FilePath

		resp, sendErr = p.whatsapp.SendFile(ctx, message.Phone, caption, filePath)

	default:
		finalMessage := message.Message

		resp, sendErr = p.whatsapp.SendMessage(ctx, message.Phone, finalMessage)
	}

	if sendErr != nil {
		return false, sendErr
	}

	message.APIResponse = resp

	if !resp.Success {
		return false, fmt.Errorf("WhatsApp API returned error: %s", resp.Error)
	}

	p.whatsapp.SendTypingPresence(ctx, message.Phone, false)

	if filePath != "" {
		if err := os.Remove(filePath); err != nil {
			p.logger.WithError(err).WithField("file_path", filePath).Warn("Failed to delete temporary file after sending")
		}
	}

	return true, nil
}

func (p *Processor) updateMessageResult(ctx context.Context, message *models.Message, success bool, sendError error, redisMsg *RedisMessage) (models.MessageStatus, error) {
	var status models.MessageStatus
	var additionalData map[string]interface{}

	if success {
		status = models.StatusSent
		additionalData = map[string]interface{}{
			"sent_at": time.Now().UTC(),
		}

		if message.APIResponse != nil {
			if resp, ok := message.APIResponse.(*models.WhatsAppResponse); ok {
				additionalData["api_response"] = resp

				if resp.Results != nil && resp.Results.MessageID != "" {
					additionalData["message_id"] = resp.Results.MessageID
				} else if resp.MessageID != "" {
					additionalData["message_id"] = resp.MessageID
				} else if resp.ID != "" {
					additionalData["message_id"] = resp.ID
				}
			} else {
				additionalData["api_response"] = message.APIResponse
			}
		}
	} else {
		shouldRetry, err := p.db.IncrementAttempts(ctx, message.ID)
		if err != nil {
			p.logger.WithError(err).WithField("message_id", shortenID(message.ID)).Error("Failed to increment attempts")
			return status, fmt.Errorf("failed to increment attempts: %w", err)
		}

		if shouldRetry {
			baseDelay := 5 * time.Second
			multiplier := 1 << uint(message.Attempts-1)
			backoffDelay := time.Duration(int64(baseDelay) * int64(multiplier))

			if backoffDelay > 5*time.Minute {
				backoffDelay = 5 * time.Minute
			}

			executeAt := time.Now().Add(backoffDelay)

			messageJSON, err := json.Marshal(redisMsg)
			if err != nil {
				p.logger.WithError(err).WithField("message_id", shortenID(message.ID)).Error("Failed to marshal message for delayed retry")
				status = models.StatusFailed
			} else {
				err = p.redis.ScheduleDelayedMessage(ctx, messageJSON, executeAt)
				if err != nil {
					p.logger.WithError(err).WithField("message_id", shortenID(message.ID)).Error("Failed to schedule delayed retry")
					status = models.StatusFailed
				} else {
					p.logger.WithFields(logrus.Fields{
						"message_id": shortenID(message.ID),
						"attempts":   message.Attempts,
						"delay":      backoffDelay,
						"execute_at": executeAt,
					}).Info("Scheduled message for delayed retry with exponential backoff")
					status = models.StatusPending
					atomic.AddInt64(&p.stats.RetryCount, 1)
					return status, nil
				}
			}
		} else {
			status = models.StatusFailed
		}

		errorMsg := "Unknown error"
		if sendError != nil {
			errorMsg = sendError.Error()
		}
		additionalData = map[string]interface{}{
			"error_message": errorMsg,
		}
	}

	if status == models.StatusFailed && message.FilePath != "" {
		if err := os.Remove(message.FilePath); err != nil {
			p.logger.WithError(err).WithField("file_path", message.FilePath).Warn("Failed to delete temporary file after final failure")
		}
	}

	if message.APIResponse != nil {
		if resp, ok := message.APIResponse.(*models.WhatsAppResponse); ok {
			additionalData["api_response"] = resp
		} else {
			additionalData["api_response"] = message.APIResponse
		}
	}

	err := p.db.UpdateMessageStatus(ctx, message.ID, status, additionalData)
	if err != nil {
		p.logger.WithError(err).WithField("message_id", shortenID(message.ID)).Error("Failed to update final status in database")
	}

	return status, err
}

func (p *Processor) GetStats() *ProcessingStats {
	return p.stats
}

func (p *Processor) RecoverStuckMessages(ctx context.Context) error {
	p.logger.Info("Recovering stuck messages")

	err := p.db.RecoverStuckMessages(ctx, p.config.StuckMessageTimeout)
	if err != nil {
		return fmt.Errorf("failed to recover stuck messages: %w", err)
	}

	p.logger.Info("Stuck message recovery completed")
	return nil
}

func (p *Processor) GetMessageStats(ctx context.Context) (*models.MessageStats, error) {
	return p.db.GetMessageStats(ctx)
}

func (p *Processor) HealthCheck(ctx context.Context) error {
	if err := p.whatsapp.HealthCheck(ctx); err != nil {
		return fmt.Errorf("WhatsApp API health check failed: %w", err)
	}

	if _, err := p.db.GetMessageStats(ctx); err != nil {
		return fmt.Errorf("Database health check failed: %w", err)
	}

	return nil
}

func (p *Processor) CheckHealth(ctx context.Context) error {
	return p.HealthCheck(ctx)
}

func (p *Processor) startFileCleanupRoutine(ctx context.Context) {
	p.logger.Info("Started temporary file cleanup routine")

	nextRun := nextCleanupTime(time.Now())
	timer := time.NewTimer(time.Until(nextRun))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("File cleanup routine stopped")
			return
		case <-timer.C:
			if err := p.cleanupOldTempFiles(ctx); err != nil {
				p.logger.WithError(err).Warn("Failed to cleanup old temporary files")
			}
			nextRun = nextCleanupTime(time.Now())
			timer.Reset(time.Until(nextRun))
		}
	}
}

func (p *Processor) cleanupOldTempFiles(ctx context.Context) error {
	tempDir := "tmp"

	files, err := os.ReadDir(tempDir)
	if err != nil {
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	var cleanedCount int
	var errors []string

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		info, err := file.Info()
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to get file info for %s: %v", file.Name(), err))
			continue
		}

		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(tempDir, file.Name())
			hasActive, err := p.db.HasActiveMessagesForFile(ctx, filePath)
			if err != nil {
				errors = append(errors, fmt.Sprintf("failed active check for %s: %v", file.Name(), err))
				continue
			}
			if hasActive {
				p.logger.WithField("file_path", filePath).Debug("Skipping cleanup for active message file")
				continue
			}
			if err := os.Remove(filePath); err != nil {
				errors = append(errors, fmt.Sprintf("failed to delete file %s: %v", file.Name(), err))
			} else {
				cleanedCount++
			}
		}
	}

	if cleanedCount > 0 {
		p.logger.WithField("cleaned_count", cleanedCount).Info("Cleaned up old temporary files")
	}

	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "; "))
	}

	return nil
}

func (p *Processor) startDelayedMessageProcessor(ctx context.Context) {
	p.logger.Info("Started delayed message processor")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Delayed message processor stopped")
			return
		case <-ticker.C:
			if err := p.processDueDelayedMessages(ctx); err != nil {
				p.logger.WithError(err).Warn("Failed to process due delayed messages")
			}
		}
	}
}

func (p *Processor) processDueDelayedMessages(ctx context.Context) error {
	messages, err := p.redis.GetDueDelayedMessages(ctx, 10)
	if err != nil {
		return fmt.Errorf("failed to get due delayed messages: %w", err)
	}

	if len(messages) == 0 {
		return nil
	}

	p.logger.WithField("count", len(messages)).Debug("Processing due delayed messages")

	for _, msgData := range messages {
		err = p.redis.PushMessage(ctx, msgData)
		if err != nil {
			p.logger.WithError(err).Error("Failed to requeue delayed message")
			continue
		}
	}

	err = p.redis.RemoveDelayedMessages(ctx, messages)
	if err != nil {
		p.logger.WithError(err).Warn("Failed to remove processed delayed messages")
	}

	return nil
}

func nextCleanupTime(now time.Time) time.Time {
	cleanupToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 1, 0, 0, now.Location())
	if !now.Before(cleanupToday) {
		cleanupToday = cleanupToday.Add(24 * time.Hour)
	}
	return cleanupToday
}

func (p *Processor) Start(ctx context.Context) error {
	go p.startFileCleanupRoutine(ctx)

	go p.startDelayedMessageProcessor(ctx)

	return p.ProcessMessages(ctx)
}

func (p *Processor) Stop() {
	p.logger.Info("Processor shutdown requested")
}

func (p *Processor) metricsUpdater(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			depth := p.redis.GetClient().LLen(ctx, p.config.RedisQueueName).Val()
			atomic.StoreInt64(&p.stats.QueueDepth, depth)
		case <-ctx.Done():
			return
		}
	}
}
