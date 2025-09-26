package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gowhatsapp-worker/internal/config"
	"gowhatsapp-worker/internal/database"
	"gowhatsapp-worker/internal/models"
	"gowhatsapp-worker/internal/redis"

	"github.com/sirupsen/logrus"
)

type MessageService struct {
	db     database.DatabaseInterface
	redis  *redis.Client
	config *config.Config
	logger *logrus.Logger
}

func NewMessageService(db database.DatabaseInterface, redis *redis.Client, config *config.Config, logger *logrus.Logger) *MessageService {
	return &MessageService{
		db:     db,
		redis:  redis,
		config: config,
		logger: logger,
	}
}

// createCampaignAndChunks creates a campaign and splits recipients into chunks
func (s *MessageService) createCampaignAndChunks(ctx context.Context, messageType string, recipients []string, campaignToken *string) (*models.Campaign, []*models.Chunk, error) {
	var token string
	var existingCampaign *models.Campaign
	if campaignToken != nil && *campaignToken != "" {
		token = *campaignToken
		existing, err := s.db.GetCampaignByToken(ctx, token)
		if err != nil && !strings.Contains(err.Error(), "no rows") {
			return nil, nil, fmt.Errorf("failed to check existing campaign: %w", err)
		}
		if err == nil {
			existingCampaign = existing
			s.logger.WithField("campaign_id", existing.ID).Info("Found existing campaign, resuming")
		}
	} else {
		token = uuid.New().String()
	}

	if existingCampaign != nil {
		campaign := existingCampaign

		pendingChunks, err := s.db.GetPendingChunks(ctx, campaign.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get pending chunks: %w", err)
		}

		if len(pendingChunks) == 0 {
			return nil, nil, fmt.Errorf("campaign already completed, no pending chunks")
		}

		s.logger.WithFields(logrus.Fields{
			"campaign_id":    campaign.ID,
			"pending_chunks": len(pendingChunks),
		}).Info("Resuming campaign with pending chunks")

		return campaign, pendingChunks, nil
	}

	campaignID := uuid.New().String()

	campaign := &models.Campaign{
		ID:          campaignID,
		Token:       token,
		Status:      string(models.CampaignStatusPending),
		MessageType: messageType,
		TotalChunks: 0,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	chunkSize := s.config.BulkChunkSize
	if chunkSize <= 0 {
		chunkSize = 500
	}

	var chunks []*models.Chunk
	for i := 0; i < len(recipients); i += chunkSize {
		end := i + chunkSize
		if end > len(recipients) {
			end = len(recipients)
		}

		chunkRecipients := recipients[i:end]
		chunk := &models.Chunk{
			ID:          uuid.New().String(),
			CampaignID:  campaignID,
			Recipients:  chunkRecipients,
			Status:      string(models.ChunkStatusPending),
			Attempts:    0,
			MaxAttempts: 3,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		chunks = append(chunks, chunk)
	}

	campaign.TotalChunks = len(chunks)

	if err := s.db.StoreCampaign(ctx, campaign); err != nil {
		return nil, nil, fmt.Errorf("failed to store campaign: %w", err)
	}

	for _, chunk := range chunks {
		if err := s.db.StoreChunk(ctx, chunk); err != nil {
			return nil, nil, fmt.Errorf("failed to store chunk: %w", err)
		}
	}

	return campaign, chunks, nil
}

// New API request types for Aldi Kemal's WhatsApp API
type WhatsAppMessageRequest struct {
	Phone          string `json:"phone" validate:"required"`
	Message        string `json:"message" validate:"required"`
	ReplyMessageID string `json:"reply_message_id,omitempty"`
	IsForwarded    bool   `json:"is_forwarded,omitempty"`
	Duration       int    `json:"duration,omitempty"`
}

type WhatsAppMediaRequest struct {
	Phone       string `json:"phone" validate:"required"`
	Caption     string `json:"caption,omitempty"`
	ViewOnce    bool   `json:"view_once,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	Compress    bool   `json:"compress,omitempty"`
	Duration    int    `json:"duration,omitempty"`
	IsForwarded bool   `json:"is_forwarded,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
}

type WhatsAppFileRequest struct {
	Phone       string `json:"phone" validate:"required"`
	Caption     string `json:"caption,omitempty"`
	IsForwarded bool   `json:"is_forwarded,omitempty"`
	Duration    int    `json:"duration,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
}

type WhatsAppMessageBulkRequest struct {
	Phone          string   `json:"phone,omitempty"`
	Phones         []string `json:"phones,omitempty"`
	Message        string   `json:"message" validate:"required"`
	CampaignToken  *string  `json:"campaign_token,omitempty"`
	ReplyMessageID string   `json:"reply_message_id,omitempty"`
	IsForwarded    bool     `json:"is_forwarded,omitempty"`
	Duration       int      `json:"duration,omitempty"`
}

type WhatsAppMediaBulkRequest struct {
	Phone         string   `json:"phone,omitempty"`
	Phones        []string `json:"phones,omitempty"`
	Caption       string   `json:"caption,omitempty"`
	CampaignToken *string  `json:"campaign_token,omitempty"`
	ViewOnce      bool     `json:"view_once,omitempty"`
	ImageURL      string   `json:"image_url,omitempty"`
	Compress      bool     `json:"compress,omitempty"`
	Duration      int      `json:"duration,omitempty"`
	IsForwarded   bool     `json:"is_forwarded,omitempty"`
	FilePath      string   `json:"file_path,omitempty"`
}

type WhatsAppFileBulkRequest struct {
	Phone         string   `json:"phone,omitempty"`
	Phones        []string `json:"phones,omitempty"`
	Caption       string   `json:"caption,omitempty"`
	CampaignToken *string  `json:"campaign_token,omitempty"`
	IsForwarded   bool     `json:"is_forwarded,omitempty"`
	Duration      int      `json:"duration,omitempty"`
	FilePath      string   `json:"file_path,omitempty"`
}

type SendMessageResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message,omitempty"`
	Data    *MessageResponseData `json:"data,omitempty"`
	Error   string               `json:"error,omitempty"`
}

type MessageResponseData struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

func (s *MessageService) storeMessageInDatabase(ctx context.Context, message *models.Message) error {
	return s.db.StoreMessage(ctx, message)
}

// New WhatsApp API service methods
func (s *MessageService) ProcessWhatsAppMessage(ctx context.Context, req WhatsAppMessageRequest) (*SendMessageResponse, error) {
	messageID := uuid.New().String()

	message := &models.Message{
		ID:             messageID,
		Phone:          req.Phone,
		Message:        req.Message,
		MessageType:    "text",
		ReplyMessageID: req.ReplyMessageID,
		IsForwarded:    req.IsForwarded,
		Duration:       req.Duration,
		Provider:       "go-whatsapp",
		QueueStatus:    string(models.QueueStatusPending),
		Status:         string(models.StatusPending),
		Attempts:       0,
		MaxAttempts:    3,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	err := s.storeMessageInDatabase(ctx, message)
	if err != nil {
		s.logger.WithError(err).Errorf("Failed to store WhatsApp message %s in database", messageID)
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to store message: %v", err),
		}, err
	}

	messageData := map[string]interface{}{
		"id":               messageID,
		"phone":            req.Phone,
		"message":          req.Message,
		"message_type":     "text",
		"reply_message_id": req.ReplyMessageID,
		"is_forwarded":     req.IsForwarded,
		"duration":         req.Duration,
		"created_at":       message.CreatedAt.Format(time.RFC3339),
		"provider":         "go-whatsapp",
		"queue_status":     "pending",
		"status":           "pending",
		"attempts":         0,
		"max_attempts":     3,
		"retry_count":      0,
	}

	messageJSON, err := json.Marshal(messageData)
	if err != nil {
		s.logger.WithError(err).Errorf("Failed to marshal WhatsApp message %s for Redis", messageID)
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to marshal message: %v", err),
		}, err
	}

	err = s.redis.PushMessage(ctx, messageJSON)
	if err != nil {
		s.logger.WithError(err).Errorf("Failed to push WhatsApp message %s to Redis queue", messageID)
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to queue message: %v", err),
		}, err
	}

	s.logger.WithField("message_id", messageID).Debug("WhatsApp message successfully stored in DB and queued for processing")

	return &SendMessageResponse{
		Success: true,
		Message: "WhatsApp message queued for processing",
		Data: &MessageResponseData{
			MessageID: messageID,
			Status:    "queued",
		},
	}, nil
}

func (s *MessageService) ProcessWhatsAppMedia(ctx context.Context, req WhatsAppMediaRequest) (*SendMessageResponse, error) {
	messageID := uuid.New().String()

	message := &models.Message{
		ID:          messageID,
		Phone:       req.Phone,
		Message:     req.Caption,
		MessageType: "image",
		Caption:     req.Caption,
		FilePath:    req.FilePath,
		ImageURL:    req.ImageURL,
		ViewOnce:    req.ViewOnce,
		Compress:    req.Compress,
		IsForwarded: req.IsForwarded,
		Duration:    req.Duration,
		Provider:    "go-whatsapp",
		QueueStatus: string(models.QueueStatusPending),
		Status:      string(models.StatusPending),
		Attempts:    0,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.storeMessageInDatabase(ctx, message); err != nil {
		s.logger.WithError(err).Errorf("Failed to store WhatsApp media %s in database", messageID)
		return &SendMessageResponse{Success: false, Error: fmt.Sprintf("Failed to store message: %v", err)}, err
	}

	messageData := map[string]interface{}{
		"id":           messageID,
		"phone":        req.Phone,
		"message":      req.Caption,
		"message_type": "image",
		"caption":      req.Caption,
		"view_once":    req.ViewOnce,
		"image_url":    req.ImageURL,
		"compress":     req.Compress,
		"duration":     req.Duration,
		"is_forwarded": req.IsForwarded,
		"file_path":    req.FilePath,
		"created_at":   message.CreatedAt.Format(time.RFC3339),
		"provider":     "go-whatsapp",
		"queue_status": "pending",
		"status":       "pending",
		"attempts":     0,
		"max_attempts": 3,
		"retry_count":  0,
	}

	messageJSON, err := json.Marshal(messageData)
	if err != nil {
		s.logger.WithError(err).Errorf("Failed to marshal WhatsApp media %s for Redis", messageID)
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to marshal message: %v", err),
		}, err
	}

	s.logger.WithFields(logrus.Fields{
		"message_id":   messageID,
		"message_type": "image",
		"redis_data":   string(messageJSON),
	}).Info("Storing image message in Redis")

	err = s.redis.PushMessage(ctx, messageJSON)
	if err != nil {
		s.logger.WithError(err).Errorf("Failed to push WhatsApp media %s to Redis queue", messageID)
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to queue message: %v", err),
		}, err
	}

	s.logger.WithField("message_id", messageID).Info("WhatsApp media successfully queued for processing")
	s.logger.WithField("message_id", messageID).Debug("WhatsApp message successfully stored in DB and queued for processing")

	return &SendMessageResponse{
		Success: true,
		Message: "WhatsApp media queued for processing",
		Data: &MessageResponseData{
			MessageID: messageID,
			Status:    "queued",
		},
	}, nil
}

func (s *MessageService) ProcessWhatsAppFile(ctx context.Context, req WhatsAppFileRequest) (*SendMessageResponse, error) {
	messageID := uuid.New().String()

	message := &models.Message{
		ID:          messageID,
		Phone:       req.Phone,
		Message:     req.Caption,
		MessageType: "file",
		Caption:     req.Caption,
		FilePath:    req.FilePath,
		IsForwarded: req.IsForwarded,
		Duration:    req.Duration,
		Provider:    "go-whatsapp",
		QueueStatus: string(models.QueueStatusPending),
		Status:      string(models.StatusPending),
		Attempts:    0,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.storeMessageInDatabase(ctx, message); err != nil {
		s.logger.WithError(err).Errorf("Failed to store WhatsApp file %s in database", messageID)
		return &SendMessageResponse{Success: false, Error: fmt.Sprintf("Failed to store message: %v", err)}, err
	}

	messageData := map[string]interface{}{
		"id":           messageID,
		"phone":        req.Phone,
		"message":      req.Caption,
		"message_type": "file",
		"caption":      req.Caption,
		"duration":     req.Duration,
		"is_forwarded": req.IsForwarded,
		"file_path":    req.FilePath,
		"created_at":   message.CreatedAt.Format(time.RFC3339),
		"provider":     "go-whatsapp",
		"queue_status": "pending",
		"status":       "pending",
		"attempts":     0,
		"max_attempts": 3,
		"retry_count":  0,
	}

	messageJSON, err := json.Marshal(messageData)
	if err != nil {
		s.logger.WithError(err).Errorf("Failed to marshal WhatsApp file %s for Redis", messageID)
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to marshal message: %v", err),
		}, err
	}

	s.logger.WithFields(logrus.Fields{
		"message_id":   messageID,
		"message_type": "file",
		"redis_data":   string(messageJSON),
	}).Info("Storing file message in Redis")

	err = s.redis.PushMessage(ctx, messageJSON)
	if err != nil {
		s.logger.WithError(err).Errorf("Failed to push WhatsApp file %s to Redis queue", messageID)
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to queue message: %v", err),
		}, err
	}

	s.logger.WithField("message_id", messageID).Info("WhatsApp file successfully queued for processing")
	s.logger.WithField("message_id", messageID).Debug("WhatsApp message successfully stored in DB and queued for processing")

	return &SendMessageResponse{
		Success: true,
		Message: "WhatsApp file queued for processing",
		Data: &MessageResponseData{
			MessageID: messageID,
			Status:    "queued",
		},
	}, nil
}

func (s *MessageService) ProcessWhatsAppMessageBulk(ctx context.Context, req WhatsAppMessageBulkRequest) (*SendMessageResponse, error) {
	var targets []string
	if len(req.Phones) > 0 {
		targets = req.Phones
	} else if req.Phone != "" {
		phones := strings.Split(req.Phone, ",")
		for _, phone := range phones {
			phone = strings.TrimSpace(phone)
			if phone != "" {
				targets = append(targets, phone)
			}
		}
	}

	if len(targets) == 0 {
		return &SendMessageResponse{
			Success: false,
			Error:   "No valid phone numbers provided",
		}, fmt.Errorf("no valid phone numbers")
	}

	var campaign *models.Campaign
	var isResume bool
	if req.CampaignToken != nil && *req.CampaignToken != "" {
		existing, err := s.db.GetCampaignByToken(ctx, *req.CampaignToken)
		if err != nil && !strings.Contains(err.Error(), "no rows") {
			return &SendMessageResponse{
				Success: false,
				Error:   fmt.Sprintf("Failed to check campaign token: %v", err),
			}, err
		}
		if err == nil {
			isResume = true
			campaign = existing
		}
	}

	campaign, chunks, err := s.createCampaignAndChunks(ctx, "text", targets, req.CampaignToken)
	if err != nil {
		s.logger.WithError(err).Error("Failed to create campaign and chunks")
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to create campaign: %v", err),
		}, err
	}

	if isResume {
		targets = []string{}
		for _, chunk := range chunks {
			targets = append(targets, chunk.Recipients...)
		}
		s.logger.WithFields(logrus.Fields{
			"campaign_token":     *req.CampaignToken,
			"resumed_recipients": len(targets),
		}).Info("Resuming campaign with recipients from pending chunks")
	}

	var messages []*models.Message
	var messageDatas []map[string]interface{}

	chunkIndex := 0
	recipientIndex := 0
	currentChunk := chunks[chunkIndex]

	for _, target := range targets {
		if recipientIndex >= len(currentChunk.Recipients) {
			chunkIndex++
			if chunkIndex >= len(chunks) {
				break
			}
			currentChunk = chunks[chunkIndex]
			recipientIndex = 0
		}

		messageID := uuid.New().String()
		message := &models.Message{
			ID:             messageID,
			Phone:          target,
			Message:        req.Message,
			MessageType:    "text",
			CampaignID:     &campaign.ID,
			ChunkID:        &currentChunk.ID,
			ReplyMessageID: req.ReplyMessageID,
			IsForwarded:    req.IsForwarded,
			Duration:       req.Duration,
			Provider:       "go-whatsapp",
			QueueStatus:    string(models.QueueStatusPending),
			Status:         string(models.StatusPending),
			Attempts:       0,
			MaxAttempts:    3,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		messages = append(messages, message)

		messageData := map[string]interface{}{
			"id":               messageID,
			"phone":            target,
			"message":          req.Message,
			"message_type":     "text",
			"campaign_id":      campaign.ID,
			"chunk_id":         currentChunk.ID,
			"reply_message_id": req.ReplyMessageID,
			"is_forwarded":     req.IsForwarded,
			"duration":         req.Duration,
			"created_at":       message.CreatedAt.Format(time.RFC3339),
			"provider":         "go-whatsapp",
			"queue_status":     "pending",
			"status":           "pending",
			"attempts":         0,
			"max_attempts":     3,
			"retry_count":      0,
		}
		messageDatas = append(messageDatas, messageData)
		recipientIndex++
	}

	err = s.db.BatchStoreMessages(ctx, messages)
	if err != nil {
		s.logger.WithError(err).Error("Failed to batch store WhatsApp messages in database")
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to store messages: %v", err),
		}, err
	}

	var successCount int
	var errors []string
	for i, messageData := range messageDatas {
		messageJSON, err := json.Marshal(messageData)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Failed to marshal message %d: %v", i, err))
			continue
		}
		err = s.redis.PushMessage(ctx, messageJSON)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Failed to push message %d to Redis: %v", i, err))
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		return &SendMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Successfully queued %d out of %d messages in campaign %s", successCount, len(targets), campaign.Token),
			Data: &MessageResponseData{
				MessageID: campaign.Token,
				Status:    "queued",
			},
		}, nil
	}

	return &SendMessageResponse{
		Success: false,
		Error:   fmt.Sprintf("Failed to queue any messages. Errors: %s", strings.Join(errors, "; ")),
	}, fmt.Errorf("failed to queue any messages")
}

func (s *MessageService) ProcessWhatsAppMediaBulk(ctx context.Context, req WhatsAppMediaBulkRequest) (*SendMessageResponse, error) {
	phones := strings.Split(req.Phone, ",")
	var targets []string
	for _, phone := range phones {
		phone = strings.TrimSpace(phone)
		if phone != "" {
			targets = append(targets, phone)
		}
	}

	if len(targets) == 0 {
		return &SendMessageResponse{
			Success: false,
			Error:   "No valid phone numbers provided",
		}, fmt.Errorf("no valid phone numbers")
	}

	var messages []*models.Message
	var messageDatas []map[string]interface{}
	var duplicationErrors []string

	baseFilePath := strings.TrimSpace(req.FilePath)
	multipleWithFile := baseFilePath != "" && len(targets) > 1

	for idx, target := range targets {
		currentFilePath := baseFilePath
		if multipleWithFile && idx > 0 {
			clonedPath, err := s.cloneTempFile(baseFilePath)
			if err != nil {
				s.logger.WithError(err).WithField("target", target).Error("Failed to duplicate media file for bulk recipient")
				duplicationErrors = append(duplicationErrors, fmt.Sprintf("%s: %v", target, err))
				continue
			}
			currentFilePath = clonedPath
		}

		messageID := uuid.New().String()
		message := &models.Message{
			ID:          messageID,
			Phone:       target,
			Message:     req.Caption,
			MessageType: "image",
			Caption:     req.Caption,
			FilePath:    currentFilePath,
			ImageURL:    req.ImageURL,
			ViewOnce:    req.ViewOnce,
			Compress:    req.Compress,
			IsForwarded: req.IsForwarded,
			Duration:    req.Duration,
			Provider:    "go-whatsapp",
			QueueStatus: string(models.QueueStatusPending),
			Status:      string(models.StatusPending),
			Attempts:    0,
			MaxAttempts: 3,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		messages = append(messages, message)

		messageData := map[string]interface{}{
			"id":           messageID,
			"phone":        target,
			"message":      req.Caption,
			"message_type": "image",
			"caption":      req.Caption,
			"file_path":    currentFilePath,
			"image_url":    req.ImageURL,
			"view_once":    req.ViewOnce,
			"compress":     req.Compress,
			"is_forwarded": req.IsForwarded,
			"duration":     req.Duration,
			"created_at":   message.CreatedAt.Format(time.RFC3339),
			"provider":     "go-whatsapp",
			"queue_status": "pending",
			"status":       "pending",
			"attempts":     0,
			"max_attempts": 3,
			"retry_count":  0,
		}
		messageDatas = append(messageDatas, messageData)
	}

	if len(messages) == 0 {
		errMsg := "failed to prepare media messages"
		if len(duplicationErrors) > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, strings.Join(duplicationErrors, "; "))
		}
		return &SendMessageResponse{
			Success: false,
			Error:   errMsg,
		}, fmt.Errorf(errMsg)
	}

	err := s.db.BatchStoreMessages(ctx, messages)
	if err != nil {
		s.logger.WithError(err).Error("Failed to batch store WhatsApp media messages in database")
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to store messages: %v", err),
		}, err
	}

	var successCount int
	var queueErrors []string
	for i, messageData := range messageDatas {
		messageJSON, err := json.Marshal(messageData)
		if err != nil {
			queueErrors = append(queueErrors, fmt.Sprintf("marshal message %d: %v", i, err))
			continue
		}
		err = s.redis.PushMessage(ctx, messageJSON)
		if err != nil {
			queueErrors = append(queueErrors, fmt.Sprintf("push message %d: %v", i, err))
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		preparedCount := len(messageDatas)
		totalRequested := len(targets)
		skippedDuringPrep := totalRequested - preparedCount
		msg := fmt.Sprintf("Successfully queued %d out of %d prepared media messages", successCount, preparedCount)
		if skippedDuringPrep > 0 {
			msg = fmt.Sprintf("%s (skipped %d targets during file preparation)", msg, skippedDuringPrep)
		}
		if len(queueErrors) > 0 {
			msg = fmt.Sprintf("%s (encountered %d queue errors)", msg, len(queueErrors))
		}
		return &SendMessageResponse{
			Success: true,
			Message: msg,
			Data: &MessageResponseData{
				MessageID: fmt.Sprintf("bulk-media-%d", time.Now().Unix()),
				Status:    "queued",
			},
		}, nil
	}

	allErrors := append([]string{}, duplicationErrors...)
	allErrors = append(allErrors, queueErrors...)
	errMsg := "failed to queue any messages"
	if len(allErrors) > 0 {
		errMsg = fmt.Sprintf("%s: %s", errMsg, strings.Join(allErrors, "; "))
	}
	return &SendMessageResponse{
		Success: false,
		Error:   errMsg,
	}, fmt.Errorf(errMsg)
}

func (s *MessageService) ProcessWhatsAppFileBulk(ctx context.Context, req WhatsAppFileBulkRequest) (*SendMessageResponse, error) {
	phones := strings.Split(req.Phone, ",")
	var targets []string
	for _, phone := range phones {
		phone = strings.TrimSpace(phone)
		if phone != "" {
			targets = append(targets, phone)
		}
	}

	if len(targets) == 0 {
		return &SendMessageResponse{
			Success: false,
			Error:   "No valid phone numbers provided",
		}, fmt.Errorf("no valid phone numbers")
	}

	var messages []*models.Message
	var messageDatas []map[string]interface{}
	var duplicationErrors []string

	baseFilePath := strings.TrimSpace(req.FilePath)
	multipleWithFile := baseFilePath != "" && len(targets) > 1

	for idx, target := range targets {
		currentFilePath := baseFilePath
		if multipleWithFile && idx > 0 {
			clonedPath, err := s.cloneTempFile(baseFilePath)
			if err != nil {
				s.logger.WithError(err).WithField("target", target).Error("Failed to duplicate file for bulk recipient")
				duplicationErrors = append(duplicationErrors, fmt.Sprintf("%s: %v", target, err))
				continue
			}
			currentFilePath = clonedPath
		}

		messageID := uuid.New().String()
		message := &models.Message{
			ID:          messageID,
			Phone:       target,
			Message:     req.Caption,
			MessageType: "file",
			Caption:     req.Caption,
			FilePath:    currentFilePath,
			IsForwarded: req.IsForwarded,
			Duration:    req.Duration,
			Provider:    "go-whatsapp",
			QueueStatus: string(models.QueueStatusPending),
			Status:      string(models.StatusPending),
			Attempts:    0,
			MaxAttempts: 3,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		messages = append(messages, message)

		messageData := map[string]interface{}{
			"id":           messageID,
			"phone":        target,
			"message":      req.Caption,
			"message_type": "file",
			"caption":      req.Caption,
			"file_path":    currentFilePath,
			"is_forwarded": req.IsForwarded,
			"duration":     req.Duration,
			"created_at":   message.CreatedAt.Format(time.RFC3339),
			"provider":     "go-whatsapp",
			"queue_status": "pending",
			"status":       "pending",
			"attempts":     0,
			"max_attempts": 3,
			"retry_count":  0,
		}
		messageDatas = append(messageDatas, messageData)
	}

	if len(messages) == 0 {
		errMsg := "failed to prepare file messages"
		if len(duplicationErrors) > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, strings.Join(duplicationErrors, "; "))
		}
		return &SendMessageResponse{
			Success: false,
			Error:   errMsg,
		}, fmt.Errorf(errMsg)
	}

	err := s.db.BatchStoreMessages(ctx, messages)
	if err != nil {
		s.logger.WithError(err).Error("Failed to batch store WhatsApp file messages in database")
		return &SendMessageResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to store messages: %v", err),
		}, err
	}

	var successCount int
	var queueErrors []string
	for i, messageData := range messageDatas {
		messageJSON, err := json.Marshal(messageData)
		if err != nil {
			queueErrors = append(queueErrors, fmt.Sprintf("marshal message %d: %v", i, err))
			continue
		}
		err = s.redis.PushMessage(ctx, messageJSON)
		if err != nil {
			queueErrors = append(queueErrors, fmt.Sprintf("push message %d: %v", i, err))
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		preparedCount := len(messageDatas)
		totalRequested := len(targets)
		skippedDuringPrep := totalRequested - preparedCount
		msg := fmt.Sprintf("Successfully queued %d out of %d prepared file messages", successCount, preparedCount)
		if skippedDuringPrep > 0 {
			msg = fmt.Sprintf("%s (skipped %d targets during file preparation)", msg, skippedDuringPrep)
		}
		if len(queueErrors) > 0 {
			msg = fmt.Sprintf("%s (encountered %d queue errors)", msg, len(queueErrors))
		}
		return &SendMessageResponse{
			Success: true,
			Message: msg,
			Data: &MessageResponseData{
				MessageID: fmt.Sprintf("bulk-file-%d", time.Now().Unix()),
				Status:    "queued",
			},
		}, nil
	}

	allErrors := append([]string{}, duplicationErrors...)
	allErrors = append(allErrors, queueErrors...)
	errMsg := "failed to queue any messages"
	if len(allErrors) > 0 {
		errMsg = fmt.Sprintf("%s: %s", errMsg, strings.Join(allErrors, "; "))
	}
	return &SendMessageResponse{
		Success: false,
		Error:   errMsg,
	}, fmt.Errorf(errMsg)
}

func (s *MessageService) cloneTempFile(originalPath string) (string, error) {
	if originalPath == "" {
		return "", fmt.Errorf("original path is empty")
	}

	dir := filepath.Dir(originalPath)
	ext := filepath.Ext(originalPath)
	base := strings.TrimSuffix(filepath.Base(originalPath), ext)
	if base == "" {
		base = "media"
	}

	newName := fmt.Sprintf("%s_%s%s", base, uuid.New().String(), ext)
	newPath := filepath.Join(dir, newName)

	if err := os.Link(originalPath, newPath); err == nil {
		return newPath, nil
	} else {
		s.logger.WithError(err).WithFields(logrus.Fields{
			"original": originalPath,
			"clone":    newPath,
		}).Debug("Hard link clone failed, falling back to file copy")
	}

	src, err := os.Open(originalPath)
	if err != nil {
		return "", fmt.Errorf("open source file: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(newPath)
	if err != nil {
		return "", fmt.Errorf("create cloned file: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(newPath)
		return "", fmt.Errorf("copy file content: %w", err)
	}

	if err := dst.Close(); err != nil {
		os.Remove(newPath)
		return "", fmt.Errorf("close cloned file: %w", err)
	}

	return newPath, nil
}
