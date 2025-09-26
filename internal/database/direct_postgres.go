package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"gowhatsapp-worker/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/sirupsen/logrus"
)

type DirectPostgresDatabase struct {
	db     *sql.DB
	stats  models.MessageStats
	mutex  sync.RWMutex
	stopCh chan struct{}
}

func NewDirectPostgresDatabase(host string, port int, dbname, user, password, sslMode string) (*DirectPostgresDatabase, error) {
	connURL := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + dbname,
	}

	if password != "" {
		connURL.User = url.UserPassword(user, password)
	} else {
		connURL.User = url.User(user)
	}

	query := connURL.Query()
	if sslMode != "" {
		query.Set("sslmode", sslMode)
	}
	connURL.RawQuery = query.Encode()

	config, err := pgx.ParseConfig(connURL.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgx config: %w", err)
	}
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	db := stdlib.OpenDB(*config)

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	dp := &DirectPostgresDatabase{db: db, stopCh: make(chan struct{})}
	if err := dp.EnsureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ensure schema: %w", err)
	}
	logrus.Info("✅ Database schema is up to date")

	go dp.statsUpdater()

	return dp, nil
}

// EnsureSchema creates and/or evolves whatsapp_message table idempotently
func (db *DirectPostgresDatabase) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS whatsapp_message (
            id VARCHAR PRIMARY KEY,
            message_type VARCHAR NOT NULL CHECK (message_type IN ('text','image','file')),
            phone VARCHAR NOT NULL,
            message TEXT NULL,
            caption TEXT NULL,
            file_path TEXT NULL,
            image_url TEXT NULL,
            view_once BOOLEAN NOT NULL DEFAULT false,
            compress BOOLEAN NOT NULL DEFAULT false,
            is_forwarded BOOLEAN NOT NULL DEFAULT false,
            reply_message_id VARCHAR NULL,
            duration INT NULL,
            provider VARCHAR NOT NULL DEFAULT 'go-whatsapp',
            campaign_id VARCHAR NULL,
            chunk_id VARCHAR NULL,
            queue_status VARCHAR NOT NULL DEFAULT 'pending',
            status VARCHAR NOT NULL DEFAULT 'pending',
            attempts INT NOT NULL DEFAULT 0,
            max_attempts INT NOT NULL DEFAULT 3,
            api_response JSONB NULL,
            error_message TEXT NULL,
            sent_at TIMESTAMPTZ NULL,
            message_id VARCHAR NULL,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_file_path ON whatsapp_message(file_path)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_phone ON whatsapp_message(phone)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_queue_status ON whatsapp_message(queue_status)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_status ON whatsapp_message(status)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_type ON whatsapp_message(message_type)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_created_at ON whatsapp_message(created_at DESC)`,
		`ALTER TABLE whatsapp_message ADD COLUMN IF NOT EXISTS campaign_id VARCHAR NULL`,
		`ALTER TABLE whatsapp_message ADD COLUMN IF NOT EXISTS chunk_id VARCHAR NULL`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_campaign_id ON whatsapp_message(campaign_id)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_chunk_id ON whatsapp_message(chunk_id)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_campaign_status ON whatsapp_message(campaign_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_message_chunk_status ON whatsapp_message(chunk_id, status)`,
		`CREATE TABLE IF NOT EXISTS whatsapp_campaign (
            id VARCHAR PRIMARY KEY,
            token VARCHAR UNIQUE NOT NULL,
            status VARCHAR NOT NULL DEFAULT 'pending',
            message_type VARCHAR NOT NULL CHECK (message_type IN ('text','image','file')),
            total_chunks INT NOT NULL DEFAULT 0,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_campaign_token ON whatsapp_campaign(token)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_campaign_status ON whatsapp_campaign(status)`,
		`CREATE TABLE IF NOT EXISTS whatsapp_chunk (
            id VARCHAR PRIMARY KEY,
            campaign_id VARCHAR NOT NULL REFERENCES whatsapp_campaign(id) ON DELETE CASCADE,
            recipients JSONB NOT NULL,
            status VARCHAR NOT NULL DEFAULT 'pending',
            attempts INT NOT NULL DEFAULT 0,
            max_attempts INT NOT NULL DEFAULT 3,
            error_message TEXT NULL,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_chunk_campaign_id ON whatsapp_chunk(campaign_id)`,
		`CREATE INDEX IF NOT EXISTS idx_whatsapp_chunk_status ON whatsapp_chunk(status)`,
	}
	for _, s := range stmts {
		if _, err := db.db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (db *DirectPostgresDatabase) Close() error {
	close(db.stopCh)
	if db.db != nil {
		return db.db.Close()
	}
	return nil
}

func (db *DirectPostgresDatabase) updateAdditionalFields(ctx context.Context, messageID string, data map[string]interface{}) error {
	query := "UPDATE whatsapp_message SET updated_at = $1"
	args := []interface{}{time.Now().UTC()}
	argIndex := 2

	for field, value := range data {
		if field == "updated_at" || field == "status" || field == "queue_status" {
			continue
		}
		switch v := value.(type) {
		case *models.WhatsAppResponse:
			if v != nil {
				jsonData, err := json.Marshal(v)
				if err != nil {
					return fmt.Errorf("failed to marshal WhatsAppResponse: %w", err)
				}
				query += fmt.Sprintf(", %s = $%d", field, argIndex)
				args = append(args, string(jsonData))
			} else {
				query += fmt.Sprintf(", %s = $%d", field, argIndex)
				args = append(args, nil)
			}
		default:
			query += fmt.Sprintf(", %s = $%d", field, argIndex)
			args = append(args, value)
		}
		argIndex++
	}

	query += " WHERE id = $" + fmt.Sprintf("%d", argIndex)
	args = append(args, messageID)

	_, err := db.db.ExecContext(ctx, query, args...)
	return err
}

func (db *DirectPostgresDatabase) IncrementAttempts(ctx context.Context, messageID string) (bool, error) {
	query := `
		UPDATE whatsapp_message
		SET attempts = attempts + 1, updated_at = $1
		WHERE id = $2
		RETURNING attempts, max_attempts
	`

	var attempts, maxAttempts int
	err := db.db.QueryRowContext(ctx, query, time.Now().UTC(), messageID).Scan(&attempts, &maxAttempts)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unnamed prepared statement does not exist") {
		time.Sleep(10 * time.Millisecond)
		err = db.db.QueryRowContext(ctx, query, time.Now().UTC(), messageID).Scan(&attempts, &maxAttempts)
	}
	if err != nil {
		return false, fmt.Errorf("failed to increment attempts: %w", err)
	}

	shouldRetry := attempts < maxAttempts
	return shouldRetry, nil
}

func (db *DirectPostgresDatabase) GetMessageStats(ctx context.Context) (*models.MessageStats, error) {
	db.mutex.RLock()
	stats := db.stats
	db.mutex.RUnlock()

	if stats.TotalMessages == 0 && stats.PendingMessages == 0 && stats.ProcessingMessages == 0 {
		return db.getLiveMessageStats(ctx)
	}

	return &stats, nil
}

func (db *DirectPostgresDatabase) RecoverStuckMessages(ctx context.Context, timeout time.Duration) error {
	stuckTime := time.Now().UTC().Add(-timeout)

	query := `
		UPDATE whatsapp_message
		SET queue_status = 'pending', status = 'pending', updated_at = $1
		WHERE provider = 'go-whatsapp'
		  AND queue_status = 'processing'
		  AND updated_at < $2
		  AND attempts < max_attempts
	`

	result, err := db.db.ExecContext(ctx, query, time.Now().UTC(), stuckTime)
	if err != nil {
		return fmt.Errorf("failed to recover stuck messages: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		fmt.Printf("Recovered %d stuck messages\n", rowsAffected)
	}

	return nil
}

func (db *DirectPostgresDatabase) HealthCheck(ctx context.Context) error {
	return db.db.PingContext(ctx)
}

// GetDatabaseMetrics returns database performance metrics for monitoring
func (db *DirectPostgresDatabase) GetDatabaseMetrics(ctx context.Context) (*models.DatabaseMetrics, error) {
	metrics := &models.DatabaseMetrics{}

	start := time.Now()
	testID := fmt.Sprintf("health-check-%d", time.Now().UnixNano())
	_, err := db.db.ExecContext(ctx, `
		INSERT INTO whatsapp_message (
			id, message_type, phone, message, provider, queue_status, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING`,
		testID, "text", "health-check", "health check message", "system", "completed", "sent", time.Now(), time.Now())
	metrics.WriteLatency = time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("database write test failed: %w", err)
	}

	db.db.ExecContext(ctx, "DELETE FROM whatsapp_message WHERE id = $1", testID)

	err = db.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM whatsapp_message 
		WHERE queue_status = 'pending' OR status = 'pending'`).Scan(&metrics.QueueBacklog)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue backlog: %w", err)
	}

	err = db.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity").Scan(&metrics.ActiveConnections)
	if err != nil {
		metrics.ActiveConnections = -1
	}

	return metrics, nil
}
func (db *DirectPostgresDatabase) StoreMessage(ctx context.Context, message *models.Message) error {
	query := `
		INSERT INTO whatsapp_message (
			id, message_type, phone, message, caption, file_path, image_url, view_once, compress, is_forwarded, reply_message_id, duration, provider, queue_status, status,
			attempts, max_attempts, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (id) DO UPDATE SET
			message_type = EXCLUDED.message_type,
			phone = EXCLUDED.phone,
			message = EXCLUDED.message,
			caption = EXCLUDED.caption,
			file_path = EXCLUDED.file_path,
			image_url = EXCLUDED.image_url,
			view_once = EXCLUDED.view_once,
			compress = EXCLUDED.compress,
			is_forwarded = EXCLUDED.is_forwarded,
			reply_message_id = EXCLUDED.reply_message_id,
			duration = EXCLUDED.duration,
			provider = EXCLUDED.provider,
			queue_status = EXCLUDED.queue_status,
			status = EXCLUDED.status,
			attempts = EXCLUDED.attempts,
			max_attempts = EXCLUDED.max_attempts,
			updated_at = EXCLUDED.updated_at`

	_, err := db.db.ExecContext(ctx, query,
		message.ID,
		message.MessageType,
		message.Phone,
		message.Message,
		message.Caption,
		message.FilePath,
		message.ImageURL,
		message.ViewOnce,
		message.Compress,
		message.IsForwarded,
		message.ReplyMessageID,
		message.Duration,
		message.Provider,
		message.QueueStatus,
		message.Status,
		message.Attempts,
		message.MaxAttempts,
		message.CreatedAt,
		message.UpdatedAt,
	)

	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unnamed prepared statement does not exist") {
		time.Sleep(10 * time.Millisecond)
		_, err = db.db.ExecContext(ctx, query,
			message.ID,
			message.MessageType,
			message.Phone,
			message.Message,
			message.Caption,
			message.FilePath,
			message.ImageURL,
			message.ViewOnce,
			message.Compress,
			message.IsForwarded,
			message.ReplyMessageID,
			message.Duration,
			message.Provider,
			message.QueueStatus,
			message.Status,
			message.Attempts,
			message.MaxAttempts,
			message.CreatedAt,
			message.UpdatedAt,
		)
	}

	if err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	return nil
}

func (db *DirectPostgresDatabase) UpdateMessageStatus(ctx context.Context, messageID string, status models.MessageStatus, additionalData map[string]interface{}) error {
	setParts := []string{"status = $1", "queue_status = $2", "updated_at = $3"}
	args := []interface{}{string(status)}

	var queueStatus string
	switch status {
	case models.StatusSent:
		queueStatus = "sent"
	case models.StatusFailed:
		queueStatus = "failed"
	case models.StatusProcessing:
		queueStatus = "processing"
	default:
		queueStatus = "pending"
	}
	args = append(args, queueStatus, time.Now().UTC())

	argIndex := 4
	for field, value := range additionalData {
		if field == "updated_at" || field == "status" || field == "queue_status" {
			continue
		}
		switch v := value.(type) {
		case *models.WhatsAppResponse:
			if v != nil {
				jsonData, err := json.Marshal(v)
				if err != nil {
					return fmt.Errorf("failed to marshal WhatsAppResponse: %w", err)
				}
				if field == "api_response" {
					setParts = append(setParts, fmt.Sprintf("%s = $%d::jsonb", field, argIndex))
				} else {
					setParts = append(setParts, fmt.Sprintf("%s = $%d", field, argIndex))
				}
				args = append(args, string(jsonData))
			} else {
				if field == "api_response" {
					setParts = append(setParts, fmt.Sprintf("%s = $%d::jsonb", field, argIndex))
				} else {
					setParts = append(setParts, fmt.Sprintf("%s = $%d", field, argIndex))
				}
				args = append(args, nil)
			}
		default:
			if field == "api_response" {
				setParts = append(setParts, fmt.Sprintf("%s = $%d::jsonb", field, argIndex))
			} else {
				setParts = append(setParts, fmt.Sprintf("%s = $%d", field, argIndex))
			}
			args = append(args, value)
		}
		argIndex++
	}

	query := fmt.Sprintf("UPDATE whatsapp_message SET %s WHERE id = $%d", strings.Join(setParts, ", "), argIndex)
	args = append(args, messageID)

	if _, err := db.db.ExecContext(ctx, query, args...); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unnamed prepared statement does not exist") {
			time.Sleep(10 * time.Millisecond)
			if _, err2 := db.db.ExecContext(ctx, query, args...); err2 == nil {
				return nil
			}
		}
		return fmt.Errorf("failed to update message status: %w", err)
	}
	return nil
}

func (db *DirectPostgresDatabase) FetchMessageByID(ctx context.Context, messageID string) (*models.Message, error) {
	query := `
		SELECT id, message_type, phone, message, caption, file_path, image_url, view_once, compress, is_forwarded, reply_message_id, duration, provider, queue_status, status,
			   attempts, max_attempts, created_at, updated_at, sent_at, message_id, api_response, error_message
		FROM whatsapp_message
		WHERE id = $1
	`

	var message models.Message
	var phone, msg, messageType, caption, filePath, imageURL, replyMessageID string
	var viewOnce, compress, isForwarded bool
	var duration int
	err := db.db.QueryRowContext(ctx, query, messageID).Scan(
		&message.ID, &messageType, &phone, &msg, &caption, &filePath, &imageURL, &viewOnce, &compress, &isForwarded, &replyMessageID, &duration, &message.Provider,
		&message.QueueStatus, &message.Status, &message.Attempts, &message.MaxAttempts,
		&message.CreatedAt, &message.UpdatedAt, &message.SentAt, &message.MessageID,
		&message.APIResponse, &message.ErrorMessage,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to fetch message by ID: %w", err)
	}

	message.Phone = phone
	message.Message = msg
	message.MessageType = messageType
	message.Caption = caption
	message.FilePath = filePath
	message.ImageURL = imageURL
	message.ViewOnce = viewOnce
	message.Compress = compress
	message.IsForwarded = isForwarded
	message.ReplyMessageID = replyMessageID
	message.Duration = duration
	return &message, nil
}

func (db *DirectPostgresDatabase) HasActiveMessagesForFile(ctx context.Context, filePath string) (bool, error) {
	if filePath == "" {
		return false, nil
	}

	query := `SELECT EXISTS(
		SELECT 1
		FROM whatsapp_message
		WHERE file_path = $1
		  AND status IN ('pending', 'processing')
	)`

	var exists bool
	if err := db.db.QueryRowContext(ctx, query, filePath).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to check active messages for file: %w", err)
	}

	return exists, nil
}

func (db *DirectPostgresDatabase) BatchStoreMessages(ctx context.Context, messages []*models.Message) error {
	if len(messages) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	const batchSize = 500
	for i := 0; i < len(messages); i += batchSize {
		end := i + batchSize
		if end > len(messages) {
			end = len(messages)
		}
		batch := messages[i:end]

		if err := db.insertBatch(ctx, batch); err != nil {
			return fmt.Errorf("failed to insert batch %d-%d: %w", i, end-1, err)
		}
	}

	return nil
}

func (db *DirectPostgresDatabase) insertBatch(ctx context.Context, messages []*models.Message) error {
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	valueStrings := make([]string, 0, len(messages))
	valueArgs := make([]interface{}, 0, len(messages)*20)

	for i, message := range messages {
		offset := i * 20
		valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+7, offset+8, offset+9, offset+10,
			offset+11, offset+12, offset+13, offset+14, offset+15, offset+16, offset+17, offset+18, offset+19, offset+20))

		valueArgs = append(valueArgs,
			message.ID,
			message.MessageType,
			message.Phone,
			message.Message,
			message.Caption,
			message.FilePath,
			message.ImageURL,
			message.ViewOnce,
			message.Compress,
			message.IsForwarded,
			message.ReplyMessageID,
			message.Duration,
			message.Provider,
			message.QueueStatus,
			message.Status,
			message.Attempts,
			message.MaxAttempts,
			message.CreatedAt,
			message.UpdatedAt,
			message.SentAt,
		)
	}

	query := fmt.Sprintf(`
		INSERT INTO whatsapp_message (
			id, message_type, phone, message, caption, file_path, image_url,
			view_once, compress, is_forwarded, reply_message_id, duration,
			provider, queue_status, status, attempts, max_attempts,
			created_at, updated_at, sent_at
		) VALUES %s
		ON CONFLICT (id) DO NOTHING`,
		strings.Join(valueStrings, ","))

	_, err = tx.ExecContext(ctx, query, valueArgs...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "serialization_failure") {
			logrus.Warn("Serialization failure detected, retrying batch insert")
			time.Sleep(100 * time.Millisecond)
			_, err = tx.ExecContext(ctx, query, valueArgs...)
		}
		if err != nil {
			return fmt.Errorf("failed to batch insert messages: %w", err)
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Campaign methods
func (db *DirectPostgresDatabase) StoreCampaign(ctx context.Context, campaign *models.Campaign) error {
	query := `
		INSERT INTO whatsapp_campaign (id, token, status, message_type, total_chunks, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := db.db.ExecContext(ctx, query,
		campaign.ID, campaign.Token, campaign.Status, campaign.MessageType,
		campaign.TotalChunks, campaign.CreatedAt, campaign.UpdatedAt)
	return err
}

func (db *DirectPostgresDatabase) GetCampaign(ctx context.Context, campaignID string) (*models.Campaign, error) {
	query := `
		SELECT id, token, status, message_type, total_chunks, created_at, updated_at
		FROM whatsapp_campaign WHERE id = $1
	`
	var campaign models.Campaign
	err := db.db.QueryRowContext(ctx, query, campaignID).Scan(
		&campaign.ID, &campaign.Token, &campaign.Status, &campaign.MessageType,
		&campaign.TotalChunks, &campaign.CreatedAt, &campaign.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &campaign, nil
}

func (db *DirectPostgresDatabase) GetCampaignByToken(ctx context.Context, token string) (*models.Campaign, error) {
	query := `
		SELECT id, token, status, message_type, total_chunks, created_at, updated_at
		FROM whatsapp_campaign WHERE token = $1
	`
	var campaign models.Campaign
	err := db.db.QueryRowContext(ctx, query, token).Scan(
		&campaign.ID, &campaign.Token, &campaign.Status, &campaign.MessageType,
		&campaign.TotalChunks, &campaign.CreatedAt, &campaign.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &campaign, nil
}

func (db *DirectPostgresDatabase) UpdateCampaignStatus(ctx context.Context, campaignID string, status models.CampaignStatus) error {
	query := `
		UPDATE whatsapp_campaign SET status = $1, updated_at = $2 WHERE id = $3
	`
	_, err := db.db.ExecContext(ctx, query, string(status), time.Now().UTC(), campaignID)
	return err
}

// Chunk methods
func (db *DirectPostgresDatabase) StoreChunk(ctx context.Context, chunk *models.Chunk) error {
	recipientsJSON, err := json.Marshal(chunk.Recipients)
	if err != nil {
		return fmt.Errorf("failed to marshal recipients: %w", err)
	}

	query := `
		INSERT INTO whatsapp_chunk (id, campaign_id, recipients, status, attempts, max_attempts, error_message, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err = db.db.ExecContext(ctx, query,
		chunk.ID, chunk.CampaignID, string(recipientsJSON), chunk.Status,
		chunk.Attempts, chunk.MaxAttempts, chunk.ErrorMessage,
		chunk.CreatedAt, chunk.UpdatedAt)
	return err
}

func (db *DirectPostgresDatabase) GetChunk(ctx context.Context, chunkID string) (*models.Chunk, error) {
	query := `
		SELECT id, campaign_id, recipients, status, attempts, max_attempts, error_message, created_at, updated_at
		FROM whatsapp_chunk WHERE id = $1
	`
	var chunk models.Chunk
	var recipientsJSON string
	err := db.db.QueryRowContext(ctx, query, chunkID).Scan(
		&chunk.ID, &chunk.CampaignID, &recipientsJSON, &chunk.Status,
		&chunk.Attempts, &chunk.MaxAttempts, &chunk.ErrorMessage,
		&chunk.CreatedAt, &chunk.UpdatedAt)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal([]byte(recipientsJSON), &chunk.Recipients)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal recipients: %w", err)
	}

	return &chunk, nil
}

func (db *DirectPostgresDatabase) UpdateChunkStatus(ctx context.Context, chunkID string, status models.ChunkStatus, errorMessage *string) error {
	query := `
		UPDATE whatsapp_chunk SET status = $1, error_message = $2, updated_at = $3 WHERE id = $4
	`
	_, err := db.db.ExecContext(ctx, query, string(status), errorMessage, time.Now().UTC(), chunkID)
	return err
}

func (db *DirectPostgresDatabase) GetPendingChunks(ctx context.Context, campaignID string) ([]*models.Chunk, error) {
	query := `
		SELECT id, campaign_id, recipients, status, attempts, max_attempts, error_message, created_at, updated_at
		FROM whatsapp_chunk
		WHERE campaign_id = $1 AND status IN ('pending', 'failed') AND attempts < max_attempts
		ORDER BY created_at ASC
	`
	rows, err := db.db.QueryContext(ctx, query, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []*models.Chunk
	for rows.Next() {
		var chunk models.Chunk
		var recipientsJSON string
		err := rows.Scan(
			&chunk.ID, &chunk.CampaignID, &recipientsJSON, &chunk.Status,
			&chunk.Attempts, &chunk.MaxAttempts, &chunk.ErrorMessage,
			&chunk.CreatedAt, &chunk.UpdatedAt)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal([]byte(recipientsJSON), &chunk.Recipients)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal recipients: %w", err)
		}

		chunks = append(chunks, &chunk)
	}

	return chunks, nil
}

func (db *DirectPostgresDatabase) IncrementChunkAttempts(ctx context.Context, chunkID string) (bool, error) {
	query := `
		UPDATE whatsapp_chunk
		SET attempts = attempts + 1, updated_at = $1
		WHERE id = $2
		RETURNING attempts, max_attempts
	`
	var attempts, maxAttempts int
	err := db.db.QueryRowContext(ctx, query, time.Now().UTC(), chunkID).Scan(&attempts, &maxAttempts)
	if err != nil {
		return false, err
	}
	return attempts < maxAttempts, nil
}

func (db *DirectPostgresDatabase) statsUpdater() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if stats, err := db.getLiveMessageStats(context.Background()); err == nil {
				db.mutex.Lock()
				db.stats = *stats
				db.mutex.Unlock()
			} else {
				logrus.Warnf("Failed to update cached stats: %v", err)
			}
		case <-db.stopCh:
			return
		}
	}
}

func (db *DirectPostgresDatabase) getLiveMessageStats(ctx context.Context) (*models.MessageStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	query := `
		SELECT 
			COUNT(*) as total_messages,
			COUNT(CASE WHEN queue_status = 'pending' THEN 1 END) as pending_messages,
			COUNT(CASE WHEN queue_status = 'processing' THEN 1 END) as processing_messages,
			COUNT(CASE WHEN queue_status = 'sent' THEN 1 END) as sent_messages,
			COUNT(CASE WHEN queue_status = 'failed' THEN 1 END) as failed_messages,
			COALESCE(SUM(attempts), 0) as retry_count
		FROM whatsapp_message 
		WHERE provider = 'go-whatsapp'
	`

	var stats models.MessageStats
	err := db.db.QueryRowContext(ctx, query).Scan(
		&stats.TotalMessages, &stats.PendingMessages, &stats.ProcessingMessages,
		&stats.SentMessages, &stats.FailedMessages, &stats.RetryCount,
	)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("database query canceled: %w", err)
		}
		return nil, fmt.Errorf("failed to get message stats: %w", err)
	}

	return &stats, nil
}
