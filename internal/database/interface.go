package database

import (
	"context"
	"gowhatsapp-worker/internal/models"
	"time"
)

type DatabaseInterface interface {
	UpdateMessageStatus(ctx context.Context, messageID string, status models.MessageStatus, additionalData map[string]interface{}) error
	IncrementAttempts(ctx context.Context, messageID string) (bool, error)
	GetMessageStats(ctx context.Context) (*models.MessageStats, error)
	RecoverStuckMessages(ctx context.Context, timeout time.Duration) error
	StoreMessage(ctx context.Context, message *models.Message) error
	BatchStoreMessages(ctx context.Context, messages []*models.Message) error
	FetchMessageByID(ctx context.Context, messageID string) (*models.Message, error)
	HasActiveMessagesForFile(ctx context.Context, filePath string) (bool, error)
	HealthCheck(ctx context.Context) error
	StoreCampaign(ctx context.Context, campaign *models.Campaign) error
	GetCampaign(ctx context.Context, campaignID string) (*models.Campaign, error)
	GetCampaignByToken(ctx context.Context, token string) (*models.Campaign, error)
	UpdateCampaignStatus(ctx context.Context, campaignID string, status models.CampaignStatus) error
	StoreChunk(ctx context.Context, chunk *models.Chunk) error
	GetChunk(ctx context.Context, chunkID string) (*models.Chunk, error)
	UpdateChunkStatus(ctx context.Context, chunkID string, status models.ChunkStatus, errorMessage *string) error
	GetPendingChunks(ctx context.Context, campaignID string) ([]*models.Chunk, error)
	IncrementChunkAttempts(ctx context.Context, chunkID string) (bool, error)
	GetDatabaseMetrics(ctx context.Context) (*models.DatabaseMetrics, error)
	Close() error
}
