package models

import (
	"time"
)

type Message struct {
	ID             string      `json:"id"`
	Phone          string      `json:"phone"`
	Message        string      `json:"message"`
	MessageType    string      `json:"message_type"`
	Caption        string      `json:"caption"`
	FilePath       string      `json:"file_path"`
	ImageURL       string      `json:"image_url"`
	ViewOnce       bool        `json:"view_once"`
	Compress       bool        `json:"compress"`
	IsForwarded    bool        `json:"is_forwarded"`
	ReplyMessageID string      `json:"reply_message_id"`
	Duration       int         `json:"duration"`
	Provider       string      `json:"provider"`
	CampaignID     *string     `json:"campaign_id,omitempty"`
	ChunkID        *string     `json:"chunk_id,omitempty"`
	QueueStatus    string      `json:"queue_status"`
	Status         string      `json:"status"`
	Attempts       int         `json:"attempts"`
	MaxAttempts    int         `json:"max_attempts"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	SentAt         *time.Time  `json:"sent_at,omitempty"`
	MessageID      *string     `json:"message_id,omitempty"`
	APIResponse    interface{} `json:"api_response,omitempty"`
	ErrorMessage   *string     `json:"error_message,omitempty"`
}

type Campaign struct {
	ID          string    `json:"id"`
	Token       string    `json:"token"` // Unique campaign identifier
	Status      string    `json:"status"`
	MessageType string    `json:"message_type"` // "text", "media", "file"
	TotalChunks int       `json:"total_chunks"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Chunk struct {
	ID           string    `json:"id"`
	CampaignID   string    `json:"campaign_id"`
	Recipients   []string  `json:"recipients"` // JSON array of phone numbers
	Status       string    `json:"status"`     // "Pending", "InFlight", "Complete", "Failed"
	Attempts     int       `json:"attempts"`
	MaxAttempts  int       `json:"max_attempts"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ErrorMessage *string   `json:"error_message,omitempty"`
}

type CampaignStatus string

const (
	CampaignStatusPending    CampaignStatus = "pending"
	CampaignStatusProcessing CampaignStatus = "processing"
	CampaignStatusCompleted  CampaignStatus = "completed"
	CampaignStatusFailed     CampaignStatus = "failed"
)

type ChunkStatus string

const (
	ChunkStatusPending  ChunkStatus = "pending"
	ChunkStatusInFlight ChunkStatus = "in_flight"
	ChunkStatusComplete ChunkStatus = "complete"
	ChunkStatusFailed   ChunkStatus = "failed"
)

type MessageStatus string

const (
	StatusPending    MessageStatus = "pending"
	StatusProcessing MessageStatus = "processing"
	StatusSent       MessageStatus = "sent"
	StatusFailed     MessageStatus = "failed"
)

type QueueStatus string

const (
	QueueStatusPending    QueueStatus = "pending"
	QueueStatusProcessing QueueStatus = "processing"
	QueueStatusSent       QueueStatus = "sent"
	QueueStatusFailed     QueueStatus = "failed"
)

type WhatsAppResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Results *struct {
		MessageID string `json:"message_id"`
		Status    string `json:"status"`
	} `json:"results,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	ID        string `json:"id,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

type TypingPresenceRequest struct {
	Phone  string `json:"phone"`
	Action string `json:"action"`
}

type SendMessageRequest struct {
	Phone   string `json:"phone"`
	Message string `json:"message"`
}

type MessageStats struct {
	TotalMessages      int64 `json:"total_messages"`
	PendingMessages    int64 `json:"pending_messages"`
	ProcessingMessages int64 `json:"processing_messages"`
	SentMessages       int64 `json:"sent_messages"`
	FailedMessages     int64 `json:"failed_messages"`
	RetryCount         int64 `json:"retry_count"`
}

func (m *Message) IsValidStatus() bool {
	switch MessageStatus(m.Status) {
	case StatusPending, StatusProcessing, StatusSent, StatusFailed:
		return true
	default:
		return false
	}
}

func (m *Message) IsValidQueueStatus() bool {
	switch QueueStatus(m.QueueStatus) {
	case QueueStatusPending, QueueStatusProcessing, QueueStatusSent, QueueStatusFailed:
		return true
	default:
		return false
	}
}

func (m *Message) CanRetry() bool {
	return m.Attempts < m.MaxAttempts && m.Status != string(StatusSent)
}

func (m *Message) ShouldRetry() bool {
	return m.Status == string(StatusFailed) && m.CanRetry()
}

func (m *Message) IsStuck(timeout time.Duration) bool {
	if m.Status != string(StatusProcessing) {
		return false
	}

	return time.Since(m.UpdatedAt) > timeout
}

type DatabaseMetrics struct {
	WriteLatency      time.Duration `json:"write_latency"`
	QueueBacklog      int64         `json:"queue_backlog"`
	ActiveConnections int           `json:"active_connections"`
}
