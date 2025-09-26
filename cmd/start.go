package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"gowhatsapp-worker/internal/config"
	"gowhatsapp-worker/internal/redis"
	"gowhatsapp-worker/internal/server"
	"gowhatsapp-worker/internal/services"
	"gowhatsapp-worker/internal/worker"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the complete WhatsApp processing system",
	Long: `Start the complete WhatsApp processing system with:
• REST API server for message ingestion
• Redis-based message queue worker
• Real-time dashboard with comprehensive statistics
• Automatic health monitoring and recovery

This is the main entry point to run the entire system,
providing API + Redis worker + dashboard in a single command.`,
	Run: runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)

	startCmd.Flags().BoolVarP(
		&config.EnableHealthCheck,
		"health-check",
		"",
		true,
		"Enable health monitoring and stuck message recovery",
	)
	startCmd.Flags().IntVarP(
		&config.HealthCheckInterval,
		"health-interval",
		"",
		300,
		"Health check interval in seconds",
	)
	startCmd.Flags().BoolVarP(
		&config.EnableCORS,
		"cors",
		"",
		true,
		"Enable CORS for API endpoints",
	)
}

func runStart(cmd *cobra.Command, args []string) {
	logrus.Info("🚀 Starting WhatsApp Processing System...")

	db, redisClient, whatsappClient, processor := GetSharedComponentsWithRedis()

	cfg, err := config.LoadConfig()
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}

	messageService := services.NewMessageService(db, redisClient, cfg, logrus.StandardLogger())

	startServer := server.NewServer(
		cfg,
		db,
		whatsappClient,
		processor,
		messageService,
		redisClient,
		logrus.StandardLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startServer.Start(ctx); err != nil {
			logrus.Errorf("Server error: %v", err)
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := processor.Start(ctx); err != nil {
			logrus.Errorf("Message processor error: %v", err)
			cancel()
		}
	}()

	if config.EnableHealthCheck && config.HealthCheckInterval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Duration(config.HealthCheckInterval) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := startHealthCheck(ctx, startServer, processor, redisClient); err != nil {
						logrus.Warnf("System health check failed: %v", err)
					}
				}
			}
		}()
	}

	time.Sleep(2 * time.Second)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logrus.Infof("✅ System ready - API: http://%s:%s | Dashboard: http://%s:%s | Queue: %s | Workers: %d", 
		cfg.APIHost, cfg.APIPort, cfg.APIHost, cfg.APIPort, cfg.RedisQueueName, cfg.WorkerConcurrency)
	logrus.Info("🛑 Press Ctrl+C to stop")

	<-sigChan
	logrus.Info("🛑 Received shutdown signal, stopping system...")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("✅ System stopped gracefully")
	case <-shutdownCtx.Done():
		logrus.Warn("⏰ System shutdown timeout, forcing stop")
	}

	logrus.Info("🏁 System shutdown complete")
}

func startHealthCheck(ctx context.Context, startServer *server.Server, processor *worker.Processor, redisClient *redis.Client) error {
	var errors []string

	if redisClient != nil {
		if err := redisClient.Ping(ctx); err != nil {
			errors = append(errors, fmt.Sprintf("Redis health check failed: %v", err))
		}
	}

	if processor != nil {
		stats := processor.GetStats()
		if stats == nil {
			errors = append(errors, "Processor stats unavailable")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("health check failures: %v", errors)
	}

	logrus.Debug("✅ System health check passed")
	return nil
}