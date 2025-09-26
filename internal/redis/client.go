package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"gowhatsapp-worker/internal/config"
)

type Client struct {
	client *redis.Client
	config *config.Config
}

func NewClient(cfg *config.Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDatabase,
		PoolSize:     cfg.RedisPoolSize,
		MinIdleConns: cfg.RedisMinIdleConns,
		DialTimeout:  cfg.RedisDialTimeout,
		ReadTimeout:  cfg.RedisReadTimeout,
		WriteTimeout: cfg.RedisWriteTimeout,
		IdleTimeout:  cfg.RedisIdleTimeout,
		MaxConnAge:   cfg.RedisMaxConnAge,
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	c := &Client{
		client: rdb,
		config: cfg,
	}

	if !c.serverSupportsStreams(ctx) {
		return nil, fmt.Errorf("redis server does not support Streams (requires Redis >= 5.0)")
	}

	if err := c.CreateConsumerGroup(ctx); err != nil {
		upErr := strings.ToUpper(err.Error())
		if strings.Contains(upErr, "BUSYGROUP") {
		} else {
			return nil, fmt.Errorf("failed to create consumer group: %w", err)
		}
	}

	logrus.WithFields(logrus.Fields{
		"stream":   cfg.RedisQueueName,
		"group":    cfg.RedisConsumerGroup,
		"consumer": cfg.RedisConsumerName,
		"max_len":  cfg.RedisStreamMaxLen,
	}).Info("Using Redis Streams for queueing")

	return c, nil
}

// serverSupportsStreams checks Redis version via INFO server; Streams require Redis >= 5.0
func (c *Client) serverSupportsStreams(ctx context.Context) bool {
	info, err := c.client.Info(ctx, "server").Result()
	if err != nil {
		return false
	}
	lines := strings.Split(info, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "redis_version:") {
			v := strings.TrimPrefix(line, "redis_version:")
			v = strings.TrimSpace(v)
			parts := strings.SplitN(v, ".", 3)
			if len(parts) == 0 {
				return false
			}
			major, _ := strconv.Atoi(parts[0])
			if major >= 5 {
				return true
			}
			return false
		}
	}
	return false
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) GetClient() *redis.Client {
	return c.client
}

func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Client) PushMessage(ctx context.Context, messageData []byte) error {
	return c.PushMessageStream(ctx, messageData)
}

func (c *Client) GetQueueLength(ctx context.Context) (int64, error) {
	return c.GetStreamLength(ctx)
}

func (c *Client) ClearQueue(ctx context.Context) error {
	return c.client.Del(ctx, c.config.RedisQueueName).Err()
}

func (c *Client) GetQueueStats(ctx context.Context) (*QueueStats, error) {
	queueLen, err := c.GetQueueLength(ctx)
	if err != nil {
		return nil, err
	}

	pendingCount := int64(0)
	if c.config.RedisConsumerGroup != "" {
		if pendingInfo, err := c.client.XPending(ctx, c.config.RedisQueueName, c.config.RedisConsumerGroup).Result(); err == nil {
			if pendingInfo != nil {
				pendingCount = pendingInfo.Count
			}
		} else if err != redis.Nil {
			return nil, fmt.Errorf("failed to get Redis pending stats: %w", err)
		}
	}

	info, err := c.client.Info(ctx, "stats").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis stats: %w", err)
	}

	stats := &QueueStats{
		QueueLength:  queueLen,
		PendingCount: pendingCount,
		RedisInfo:    info,
		LastUpdated:  time.Now(),
		QueueName:    c.config.RedisQueueName,
	}

	return stats, nil
}

type QueueStats struct {
	QueueLength  int64     `json:"queue_length"`
	PendingCount int64     `json:"pending_count"`
	RedisInfo    string    `json:"redis_info"`
	LastUpdated  time.Time `json:"last_updated"`
	QueueName    string    `json:"queue_name"`
}

// ===== Streams-specific APIs =====

// PushMessageStream enqueues message data into a Redis Stream.
func (c *Client) PushMessageStream(ctx context.Context, messageData []byte) error {
	args := &redis.XAddArgs{
		Stream: c.config.RedisQueueName,
		ID:     "*",
		Values: map[string]interface{}{"data": messageData},
		MaxLen: c.config.RedisStreamMaxLen,
		Approx: true,
	}
	return c.client.XAdd(ctx, args).Err()
}

// PopMessageWithID fetches one message from the stream using consumer group, returning payload and message ID.
func (c *Client) PopMessageWithID(ctx context.Context, timeout time.Duration) ([]byte, string, error) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.config.RedisConsumerGroup,
		Consumer: c.config.RedisConsumerName,
		Streams:  []string{c.config.RedisQueueName, ">"},
		Count:    1,
		Block:    timeout,
		NoAck:    false,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("XREADGROUP failed: %w", err)
	}
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return nil, "", redis.Nil
	}
	msg := streams[0].Messages[0]
	raw, ok := msg.Values["data"]
	if !ok {
		_ = c.client.XAck(ctx, c.config.RedisQueueName, c.config.RedisConsumerGroup, msg.ID).Err()
		return nil, "", fmt.Errorf("stream message missing 'data' field: %v", msg.Values)
	}

	var payload []byte
	switch v := raw.(type) {
	case string:
		payload = []byte(v)
	case []byte:
		payload = v
	default:
		payload = []byte(fmt.Sprintf("%v", v))
	}
	return payload, msg.ID, nil
}

// AckMessageStream acknowledges a processed message in the stream.
func (c *Client) AckMessageStream(ctx context.Context, messageID string) error {
	return c.client.XAck(ctx, c.config.RedisQueueName, c.config.RedisConsumerGroup, messageID).Err()
}

// CreateConsumerGroup ensures the consumer group exists (and stream too).
func (c *Client) CreateConsumerGroup(ctx context.Context) error {
	return c.client.XGroupCreateMkStream(ctx, c.config.RedisQueueName, c.config.RedisConsumerGroup, "$").Err()
}

// GetStreamLength returns XLEN of the stream.
func (c *Client) GetStreamLength(ctx context.Context) (int64, error) {
	return c.client.XLen(ctx, c.config.RedisQueueName).Result()
}

// TrimStream trims the stream to configured max length.
func (c *Client) TrimStream(ctx context.Context) error {
	const trimLimit int64 = 1000
	return c.client.XTrimMaxLenApprox(ctx, c.config.RedisQueueName, c.config.RedisStreamMaxLen, trimLimit).Err()
}

// ReclaimIdleMessages claims up to 'count' messages that have been pending for at least 'minIdle'.
// It returns the claimed messages so callers can decide whether to requeue/ack.
func (c *Client) ReclaimIdleMessages(ctx context.Context, minIdle time.Duration, count int64) ([]redis.XMessage, error) {
	pendings, err := c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: c.config.RedisQueueName,
		Group:  c.config.RedisConsumerGroup,
		Start:  "-",
		End:    "+",
		Count:  count,
		Idle:   minIdle,
	}).Result()
	if err != nil {
		return nil, err
	}
	if len(pendings) == 0 {
		return []redis.XMessage{}, nil
	}
	ids := make([]string, 0, len(pendings))
	for _, p := range pendings {
		ids = append(ids, p.ID)
	}
	msgs, err := c.client.XClaim(ctx, &redis.XClaimArgs{
		Stream:   c.config.RedisQueueName,
		Group:    c.config.RedisConsumerGroup,
		Consumer: c.config.RedisConsumerName,
		MinIdle:  minIdle,
		Messages: ids,
	}).Result()
	if err != nil {
		return nil, err
	}
	return msgs, nil
}

// ScheduleDelayedMessage adds a message to the delayed queue with a specific execution time
func (c *Client) ScheduleDelayedMessage(ctx context.Context, messageData []byte, executeAt time.Time) error {
	delayedQueueKey := c.config.RedisQueueName + ":delayed"
	score := float64(executeAt.Unix())

	err := c.client.ZAdd(ctx, delayedQueueKey, &redis.Z{
		Score:  score,
		Member: messageData,
	}).Err()

	return err
}

// GetDueDelayedMessages retrieves messages that are due for execution
func (c *Client) GetDueDelayedMessages(ctx context.Context, maxCount int64) ([][]byte, error) {
	delayedQueueKey := c.config.RedisQueueName + ":delayed"
	now := float64(time.Now().Unix())

	result, err := c.client.ZRangeByScoreWithScores(ctx, delayedQueueKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatFloat(now, 'f', -1, 64),
		Count: maxCount,
	}).Result()

	if err != nil {
		return nil, err
	}

	messages := make([][]byte, 0, len(result))
	for _, z := range result {
		if msg, ok := z.Member.(string); ok {
			messages = append(messages, []byte(msg))
		}
	}

	return messages, nil
}

// RemoveDelayedMessages removes processed delayed messages
func (c *Client) RemoveDelayedMessages(ctx context.Context, messages [][]byte) error {
	delayedQueueKey := c.config.RedisQueueName + ":delayed"

	members := make([]interface{}, len(messages))
	for i, msg := range messages {
		members[i] = msg
	}

	return c.client.ZRem(ctx, delayedQueueKey, members...).Err()
}
