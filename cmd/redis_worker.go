package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"gowhatsapp-worker/internal/config"
	"gowhatsapp-worker/internal/redis"
	"gowhatsapp-worker/internal/worker"
)

var redisWorkerCmd = &cobra.Command{
	Use:   "redis-worker",
	Short: "Start the Redis-based WhatsApp message processing worker",
	Long: `Start the high-performance WhatsApp message processing worker using Redis queue.

The message processor will:
• Consume messages from Redis queue (blocking)
• Process messages with natural delays and rate limiting
• Handle retries with exponential backoff
• Store messages in database with full tracking
• Support text, image, and file message types
• Provide comprehensive logging and statistics
• Support horizontal scaling with multiple worker instances`,
	Run: runRedisWorker,
}

func init() {
	rootCmd.AddCommand(redisWorkerCmd)

	redisWorkerCmd.Flags().BoolVarP(
		&config.EnableHealthCheck,
		"health-check",
		"",
		true,
		"Enable health monitoring and stuck message recovery",
	)
	redisWorkerCmd.Flags().IntVarP(
		&config.HealthCheckInterval,
		"health-interval",
		"",
		300,
		"Health check interval in seconds",
	)
}

func runRedisWorker(cmd *cobra.Command, args []string) {
	logrus.Info("Starting Redis-based WhatsApp message processing worker...")

	db, whatsappClient, _ := GetSharedComponents()

	cfg, err := config.LoadConfig()
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}

	redisClient, err := redis.NewClient(cfg)
	if err != nil {
		logrus.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer redisClient.Close()

	processor := worker.NewProcessor(cfg, db, redisClient, whatsappClient, logrus.StandardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := processor.Start(ctx); err != nil {
			logrus.Errorf("Message processor error: %v", err)
			cancel()
		}
	}()

	if config.EnableHealthCheck && config.HealthCheckInterval > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(config.HealthCheckInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := processorHealthCheck(ctx, processor, redisClient); err != nil {
						logrus.Warnf("Message processor health check failed: %v", err)
					}
				}
			}
		}()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logrus.Infof("Message processor started successfully")
	logrus.Infof("Redis queue: %s", cfg.RedisQueueName)
	logrus.Infof("Redis host: %s:%s", cfg.RedisHost, cfg.RedisPort)
	logrus.Infof("Max retry attempts: %d", cfg.RedisQueueMaxRetries)
	logrus.Infof("Batch size: %d", cfg.RedisQueueBatchSize)
	logrus.Infof("Health monitoring: %v", config.EnableHealthCheck)
	if config.EnableHealthCheck {
		logrus.Infof("Health check interval: %d seconds", config.HealthCheckInterval)
	}
	logrus.Info("Press Ctrl+C to stop the message processor")

	<-sigChan
	logrus.Info("Received shutdown signal, stopping message processor...")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	done := make(chan struct{})
	go func() {
		processor.Stop()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("Message processor stopped gracefully")
	case <-shutdownCtx.Done():
		logrus.Warn("Message processor shutdown timeout, forcing stop")
	}

	logrus.Info("Message processor shutdown complete")
}

func processorHealthCheck(ctx context.Context, processor *worker.Processor, redisClient *redis.Client) error {
	if err := redisClient.Ping(ctx); err != nil {
		return err
	}

	stats := processor.GetStats()
	if stats == nil {
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"messages_processed": stats.MessagesProcessed,
		"messages_sent":      stats.MessagesSent,
		"messages_failed":    stats.MessagesFailed,
		"retry_count":        stats.RetryCount,
		"uptime":             time.Since(stats.StartTime).String(),
	}).Debug("Message processor health status")

	return nil
}
