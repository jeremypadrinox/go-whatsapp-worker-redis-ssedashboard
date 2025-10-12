package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	SupabaseURL string
	SupabaseKey string

	WhatsAppAPIURL string
	WhatsAppAPIKey string

	WorkerInterval      int  = 5
	MaxRetryAttempts    int  = 3
	EnableHealthCheck   bool = true
	HealthCheckInterval int  = 300

	APIPort    string = "8084"
	APIHost    string = "0.0.0.0"
	EnableCORS bool   = true
	APISecret  string
	EnableAPI  bool = true

	WebPort         string = "3000"
	WebHost         string = "0.0.0.0"
	EnableWebSocket bool   = true
	WebSecret       string
	EnableWeb       bool = true

	LogLevel string = "info"
)

type Config struct {
	SupabaseURL        string `mapstructure:"SUPABASE_URL"`
	SupabaseServiceKey string `mapstructure:"SUPABASE_SERVICE_ROLE_KEY"`

	DatabaseHost     string `mapstructure:"DATABASE_HOST"`
	DatabasePort     int    `mapstructure:"DATABASE_PORT"`
	DatabaseName     string `mapstructure:"DATABASE_NAME"`
	DatabaseUser     string `mapstructure:"DATABASE_USER"`
	DatabasePassword string `mapstructure:"DATABASE_PASSWORD"`
	DatabaseSSLMode  string `mapstructure:"DATABASE_SSL_MODE"`

	WhatsAppBaseURL   string   `mapstructure:"WHATSAPP_BASE_URL"`
	WhatsAppAuth      string   `mapstructure:"WHATSAPP_AUTH"`
	WhatsAppEndpoints []string `mapstructure:"WHATSAPP_ENDPOINTS"`
	WhatsAppAuths     []string `mapstructure:"WHATSAPP_AUTHS"`

	WorkerConcurrency  int           `mapstructure:"WORKER_CONCURRENCY"`
	WorkerBatchSize    int           `mapstructure:"WORKER_BATCH_SIZE"`
	WorkerPollInterval time.Duration `mapstructure:"WORKER_POLL_INTERVAL"`
	WorkerMaxRetries   int           `mapstructure:"WORKER_MAX_RETRIES"`

	RateLimitMinMessages int           `mapstructure:"RATE_LIMIT_MIN_MESSAGES"`
	RateLimitMaxMessages int           `mapstructure:"RATE_LIMIT_MAX_MESSAGES"`
	RateLimitDelayMin    time.Duration `mapstructure:"RATE_LIMIT_DELAY_MIN"`
	RateLimitDelayMax    time.Duration `mapstructure:"RATE_LIMIT_DELAY_MAX"`

	TypingDelayMin          time.Duration `mapstructure:"TYPING_DELAY_MIN"`
	TypingDelayMax          time.Duration `mapstructure:"TYPING_DELAY_MAX"`
	NaturalBreakInterval    int           `mapstructure:"NATURAL_BREAK_INTERVAL"`
	NaturalBreakDurationMin time.Duration `mapstructure:"NATURAL_BREAK_DURATION_MIN"`
	NaturalBreakDurationMax time.Duration `mapstructure:"NATURAL_BREAK_DURATION_MAX"`

	HealthCheckInterval time.Duration `mapstructure:"HEALTH_CHECK_INTERVAL"`
	StuckMessageTimeout time.Duration `mapstructure:"STUCK_MESSAGE_TIMEOUT"`

	APIHost string `mapstructure:"API_HOST"`
	APIPort string `mapstructure:"API_PORT"`

	LogLevel                    string        `mapstructure:"LOG_LEVEL"`
	NoPendingMessageLogInterval time.Duration `mapstructure:"NO_PENDING_MESSAGE_LOG_INTERVAL"`

	EnableCORS bool   `mapstructure:"ENABLE_CORS"`
	APISecret  string `mapstructure:"API_SECRET"`
	EnableAPI  bool   `mapstructure:"ENABLE_API"`

	WebPort         string `mapstructure:"WEB_PORT"`
	WebHost         string `mapstructure:"WEB_HOST"`
	EnableWebSocket bool   `mapstructure:"ENABLE_WEBSOCKET"`
	WebSecret       string `mapstructure:"WEB_SECRET"`
	EnableWeb       bool   `mapstructure:"ENABLE_WEB"`

	EnableAuth        bool   `mapstructure:"ENABLE_AUTH"`
	AuthType          string `mapstructure:"AUTH_TYPE"`
	DashboardUsername string `mapstructure:"DASHBOARD_USERNAME"`
	DashboardPassword string `mapstructure:"DASHBOARD_PASSWORD"`
	DashboardRealm    string `mapstructure:"DASHBOARD_REALM"`

	RedisHost         string        `mapstructure:"REDIS_HOST"`
	RedisPort         string        `mapstructure:"REDIS_PORT"`
	RedisPassword     string        `mapstructure:"REDIS_PASSWORD"`
	RedisDatabase     int           `mapstructure:"REDIS_DATABASE"`
	RedisPoolSize     int           `mapstructure:"REDIS_POOL_SIZE"`
	RedisMinIdleConns int           `mapstructure:"REDIS_MIN_IDLE_CONNS"`
	RedisDialTimeout  time.Duration `mapstructure:"REDIS_DIAL_TIMEOUT"`
	RedisReadTimeout  time.Duration `mapstructure:"REDIS_READ_TIMEOUT"`
	RedisWriteTimeout time.Duration `mapstructure:"REDIS_WRITE_TIMEOUT"`
	RedisIdleTimeout  time.Duration `mapstructure:"REDIS_IDLE_TIMEOUT"`
	RedisMaxConnAge   time.Duration `mapstructure:"REDIS_MAX_CONN_AGE"`

	RedisQueueName               string        `mapstructure:"REDIS_QUEUE_NAME"`
	RedisQueueProcessingTimeout  time.Duration `mapstructure:"REDIS_QUEUE_PROCESSING_TIMEOUT"`
	RedisQueueRetryDelay         time.Duration `mapstructure:"REDIS_QUEUE_RETRY_DELAY"`
	RedisQueueMaxRetries         int           `mapstructure:"REDIS_QUEUE_MAX_RETRIES"`
	RedisQueueBlockingPopTimeout time.Duration `mapstructure:"REDIS_QUEUE_BLOCKING_POP_TIMEOUT"`
	RedisQueueBatchSize          int           `mapstructure:"REDIS_QUEUE_BATCH_SIZE"`
	RedisConsumerGroup string `mapstructure:"REDIS_CONSUMER_GROUP"`
	RedisConsumerName  string `mapstructure:"REDIS_CONSUMER_NAME"`
	RedisStreamMaxLen  int64  `mapstructure:"REDIS_STREAM_MAX_LEN"`
	RateProfile            string        `mapstructure:"RATE_PROFILE"`
	RateDelayMeanMs        int           `mapstructure:"RATE_DELAY_MEAN_MS"`
	RateDelayJitterMs      int           `mapstructure:"RATE_DELAY_JITTER_MS"`
	RateBurstLimit         int           `mapstructure:"RATE_BURST_LIMIT"`
	RateCooldownSeconds    int           `mapstructure:"RATE_COOLDOWN_SECONDS"`
	BulkChunkSize int `mapstructure:"BULK_CHUNK_SIZE"`

	// Circuit Breaker settings
	CircuitBreakerEnabled          bool          `mapstructure:"CIRCUIT_BREAKER_ENABLED"`
	CircuitBreakerFailureThreshold int           `mapstructure:"CIRCUIT_BREAKER_FAILURE_THRESHOLD"`
	CircuitBreakerSuccessThreshold int           `mapstructure:"CIRCUIT_BREAKER_SUCCESS_THRESHOLD"`
	CircuitBreakerOpenTimeout      time.Duration `mapstructure:"CIRCUIT_BREAKER_OPEN_TIMEOUT"`
	CircuitBreakerHalfOpenTimeout  time.Duration `mapstructure:"CIRCUIT_BREAKER_HALF_OPEN_TIMEOUT"`
}

func LoadConfig() (*Config, error) {
	viper.AutomaticEnv()

	setDefaults()

	viper.SetConfigName(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
	}

	var config Config

	config.DatabaseHost = viper.GetString("DATABASE_HOST")
	config.DatabasePort = viper.GetInt("DATABASE_PORT")
	config.DatabaseName = viper.GetString("DATABASE_NAME")
	config.DatabaseUser = viper.GetString("DATABASE_USER")
	config.DatabasePassword = viper.GetString("DATABASE_PASSWORD")
	config.DatabaseSSLMode = viper.GetString("DATABASE_SSL_MODE")

	config.WhatsAppBaseURL = viper.GetString("WHATSAPP_BASE_URL")
	config.WhatsAppAuth = viper.GetString("WHATSAPP_AUTH")

	// Parse multiple endpoints
	endpointsStr := viper.GetString("WHATSAPP_ENDPOINTS")
	authsStr := viper.GetString("WHATSAPP_AUTHS")

	if endpointsStr != "" && authsStr != "" {
		config.WhatsAppEndpoints = strings.Split(endpointsStr, ",")
		config.WhatsAppAuths = strings.Split(authsStr, ",")
		// Trim spaces
		for i := range config.WhatsAppEndpoints {
			config.WhatsAppEndpoints[i] = strings.TrimSpace(config.WhatsAppEndpoints[i])
			config.WhatsAppAuths[i] = strings.TrimSpace(config.WhatsAppAuths[i])
		}
	} else {
		// Fallback to single endpoint
		config.WhatsAppEndpoints = []string{config.WhatsAppBaseURL}
		config.WhatsAppAuths = []string{config.WhatsAppAuth}
	}

	config.WorkerConcurrency = viper.GetInt("WORKER_CONCURRENCY")
	config.WorkerBatchSize = viper.GetInt("WORKER_BATCH_SIZE")
	config.WorkerPollInterval = viper.GetDuration("WORKER_POLL_INTERVAL")
	config.WorkerMaxRetries = viper.GetInt("WORKER_MAX_RETRIES")

	config.RateLimitMinMessages = viper.GetInt("RATE_LIMIT_MIN_MESSAGES")
	config.RateLimitMaxMessages = viper.GetInt("RATE_LIMIT_MAX_MESSAGES")
	config.RateLimitDelayMin = viper.GetDuration("RATE_LIMIT_DELAY_MIN")
	config.RateLimitDelayMax = viper.GetDuration("RATE_LIMIT_DELAY_MAX")

	config.TypingDelayMin = viper.GetDuration("TYPING_DELAY_MIN")
	config.TypingDelayMax = viper.GetDuration("TYPING_DELAY_MAX")
	config.NaturalBreakInterval = viper.GetInt("NATURAL_BREAK_INTERVAL")
	config.NaturalBreakDurationMin = viper.GetDuration("NATURAL_BREAK_DURATION_MIN")
	config.NaturalBreakDurationMax = viper.GetDuration("NATURAL_BREAK_DURATION_MAX")

	config.HealthCheckInterval = viper.GetDuration("HEALTH_CHECK_INTERVAL")
	config.StuckMessageTimeout = viper.GetDuration("STUCK_MESSAGE_TIMEOUT")

	config.APIHost = viper.GetString("API_HOST")
	config.APIPort = viper.GetString("API_PORT")
	config.LogLevel = viper.GetString("LOG_LEVEL")
	config.NoPendingMessageLogInterval = viper.GetDuration("NO_PENDING_MESSAGE_LOG_INTERVAL")

	config.EnableCORS = viper.GetBool("ENABLE_CORS")
	config.EnableAuth = viper.GetBool("ENABLE_AUTH")
	config.AuthType = viper.GetString("AUTH_TYPE")
	config.DashboardUsername = viper.GetString("DASHBOARD_USERNAME")
	config.DashboardPassword = viper.GetString("DASHBOARD_PASSWORD")
	config.DashboardRealm = viper.GetString("DASHBOARD_REALM")

	config.RedisHost = viper.GetString("REDIS_HOST")
	config.RedisPort = viper.GetString("REDIS_PORT")
	config.RedisPassword = viper.GetString("REDIS_PASSWORD")
	config.RedisDatabase = viper.GetInt("REDIS_DATABASE")
	config.RedisPoolSize = viper.GetInt("REDIS_POOL_SIZE")
	config.RedisMinIdleConns = viper.GetInt("REDIS_MIN_IDLE_CONNS")
	config.RedisDialTimeout = viper.GetDuration("REDIS_DIAL_TIMEOUT")
	config.RedisReadTimeout = viper.GetDuration("REDIS_READ_TIMEOUT")
	config.RedisWriteTimeout = viper.GetDuration("REDIS_WRITE_TIMEOUT")
	config.RedisIdleTimeout = viper.GetDuration("REDIS_IDLE_TIMEOUT")
	config.RedisMaxConnAge = viper.GetDuration("REDIS_MAX_CONN_AGE")

	config.RedisQueueName = viper.GetString("REDIS_QUEUE_NAME")
	config.RedisQueueProcessingTimeout = viper.GetDuration("REDIS_QUEUE_PROCESSING_TIMEOUT")
	config.RedisQueueRetryDelay = viper.GetDuration("REDIS_QUEUE_RETRY_DELAY")
	config.RedisQueueMaxRetries = viper.GetInt("REDIS_QUEUE_MAX_RETRIES")
	config.RedisQueueBlockingPopTimeout = viper.GetDuration("REDIS_QUEUE_BLOCKING_POP_TIMEOUT")
	config.RedisQueueBatchSize = viper.GetInt("REDIS_QUEUE_BATCH_SIZE")

	config.RedisConsumerGroup = viper.GetString("REDIS_CONSUMER_GROUP")
	config.RedisConsumerName = viper.GetString("REDIS_CONSUMER_NAME")
	config.RedisStreamMaxLen = viper.GetInt64("REDIS_STREAM_MAX_LEN")

	config.RateProfile = viper.GetString("RATE_PROFILE")
	config.RateDelayMeanMs = viper.GetInt("RATE_DELAY_MEAN_MS")
	config.RateDelayJitterMs = viper.GetInt("RATE_DELAY_JITTER_MS")
	config.RateBurstLimit = viper.GetInt("RATE_BURST_LIMIT")
	config.RateCooldownSeconds = viper.GetInt("RATE_COOLDOWN_SECONDS")

	config.BulkChunkSize = viper.GetInt("BULK_CHUNK_SIZE")

	// Circuit Breaker configuration
	config.CircuitBreakerEnabled = viper.GetBool("CIRCUIT_BREAKER_ENABLED")
	config.CircuitBreakerFailureThreshold = viper.GetInt("CIRCUIT_BREAKER_FAILURE_THRESHOLD")
	config.CircuitBreakerSuccessThreshold = viper.GetInt("CIRCUIT_BREAKER_SUCCESS_THRESHOLD")
	config.CircuitBreakerOpenTimeout = viper.GetDuration("CIRCUIT_BREAKER_OPEN_TIMEOUT")
	config.CircuitBreakerHalfOpenTimeout = viper.GetDuration("CIRCUIT_BREAKER_HALF_OPEN_TIMEOUT")

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	if config.RateProfile == "bulk" {
		validateBulkCompliance(&config)
	}

	return &config, nil
}

// validateBulkCompliance checks if bulk settings comply with WhatsApp best practices
func validateBulkCompliance(config *Config) {
	warnings := []string{}

	minDelayMs := config.RateDelayMeanMs - config.RateDelayJitterMs
	if minDelayMs < 5000 {
		warnings = append(warnings, fmt.Sprintf("minimum delay %dms too low (<5000ms)", minDelayMs))
	}

	if config.RateBurstLimit > 50 {
		warnings = append(warnings, fmt.Sprintf("burst limit %d too high (>50)", config.RateBurstLimit))
	}

	if config.RateCooldownSeconds < 180 {
		warnings = append(warnings, fmt.Sprintf("cooldown %ds too short (<180s)", config.RateCooldownSeconds))
	}

	if len(warnings) > 0 {
		logrus.WithFields(logrus.Fields{
			"profile":  "bulk",
			"warnings": warnings,
		}).Warn("Bulk messaging settings may not comply with WhatsApp policies")
	} else {
		logrus.Info("Bulk messaging settings validated for WhatsApp compliance")
	}
}

func setDefaults() {
	viper.SetDefault("WORKER_CONCURRENCY", 10)
	viper.SetDefault("WORKER_BATCH_SIZE", 50)
	viper.SetDefault("WORKER_POLL_INTERVAL", "5s")
	viper.SetDefault("WORKER_MAX_RETRIES", 3)

	viper.SetDefault("RATE_LIMIT_MIN_MESSAGES", 6)
	viper.SetDefault("RATE_LIMIT_MAX_MESSAGES", 14)
	viper.SetDefault("RATE_LIMIT_DELAY_MIN", "2s")
	viper.SetDefault("RATE_LIMIT_DELAY_MAX", "5s")

	viper.SetDefault("TYPING_DELAY_MIN", "1s")
	viper.SetDefault("TYPING_DELAY_MAX", "3s")
	viper.SetDefault("NATURAL_BREAK_INTERVAL", 50)
	viper.SetDefault("NATURAL_BREAK_DURATION_MIN", "30s")
	viper.SetDefault("NATURAL_BREAK_DURATION_MAX", "1m")

	viper.SetDefault("HEALTH_CHECK_INTERVAL", "24h")
	viper.SetDefault("STUCK_MESSAGE_TIMEOUT", "5m")

	viper.SetDefault("LOG_LEVEL", "info")
	viper.SetDefault("NO_PENDING_MESSAGE_LOG_INTERVAL", "15m")

	viper.SetDefault("DATABASE_PORT", 5432)
	viper.SetDefault("DATABASE_NAME", "postgres")
	viper.SetDefault("DATABASE_USER", "postgres")
	viper.SetDefault("DATABASE_SSL_MODE", "require")

	viper.SetDefault("API_HOST", "0.0.0.0")
	viper.SetDefault("API_PORT", "8084")

	viper.SetDefault("ENABLE_CORS", true)
	viper.SetDefault("ENABLE_API", true)

	viper.SetDefault("WEB_HOST", "0.0.0.0")
	viper.SetDefault("WEB_PORT", "3000")
	viper.SetDefault("ENABLE_WEBSOCKET", true)
	viper.SetDefault("ENABLE_WEB", true)

	viper.SetDefault("ENABLE_AUTH", true)
	viper.SetDefault("AUTH_TYPE", "basic")
	viper.SetDefault("DASHBOARD_USERNAME", "admin")
	viper.SetDefault("DASHBOARD_PASSWORD", "admin")
	viper.SetDefault("DASHBOARD_REALM", "WhatsApp Worker")

	viper.SetDefault("REDIS_HOST", "localhost")
	viper.SetDefault("REDIS_PORT", "6379")
	viper.SetDefault("REDIS_PASSWORD", "")
	viper.SetDefault("REDIS_DATABASE", 0)
	viper.SetDefault("REDIS_POOL_SIZE", 20)
	viper.SetDefault("REDIS_MIN_IDLE_CONNS", 5)
	viper.SetDefault("REDIS_DIAL_TIMEOUT", "5s")
	viper.SetDefault("REDIS_READ_TIMEOUT", "3s")
	viper.SetDefault("REDIS_WRITE_TIMEOUT", "3s")
	viper.SetDefault("REDIS_IDLE_TIMEOUT", "5m")
	viper.SetDefault("REDIS_MAX_CONN_AGE", "30m")

	viper.SetDefault("REDIS_QUEUE_NAME", "pending_messages")
	viper.SetDefault("REDIS_QUEUE_PROCESSING_TIMEOUT", "30s")
	viper.SetDefault("REDIS_QUEUE_RETRY_DELAY", "5s")
	viper.SetDefault("REDIS_QUEUE_MAX_RETRIES", 3)
	viper.SetDefault("REDIS_QUEUE_BLOCKING_POP_TIMEOUT", "0s")
	viper.SetDefault("REDIS_QUEUE_BATCH_SIZE", 10)

	viper.SetDefault("REDIS_CONSUMER_GROUP", "workers")
	viper.SetDefault("REDIS_CONSUMER_NAME", "worker-1")
	viper.SetDefault("REDIS_STREAM_MAX_LEN", 100000)
	viper.SetDefault("RATE_PROFILE", "manual")
	viper.SetDefault("RATE_DELAY_MEAN_MS", 10000)
	viper.SetDefault("RATE_DELAY_JITTER_MS", 5000)
	viper.SetDefault("RATE_BURST_LIMIT", 20)
	viper.SetDefault("RATE_COOLDOWN_SECONDS", 210)
	viper.SetDefault("BULK_CHUNK_SIZE", 500)

	// Circuit Breaker defaults
	viper.SetDefault("CIRCUIT_BREAKER_ENABLED", true)
	viper.SetDefault("CIRCUIT_BREAKER_FAILURE_THRESHOLD", 3)
	viper.SetDefault("CIRCUIT_BREAKER_SUCCESS_THRESHOLD", 2)
	viper.SetDefault("CIRCUIT_BREAKER_OPEN_TIMEOUT", "2m")
	viper.SetDefault("CIRCUIT_BREAKER_HALF_OPEN_TIMEOUT", "30s")
}

func validateConfig(config *Config) error {
	if config.DatabaseHost != "" {
		if config.WhatsAppBaseURL == "" {
			return fmt.Errorf("WHATSAPP_BASE_URL is required")
		}
		if config.WhatsAppAuth == "" {
			return fmt.Errorf("WHATSAPP_AUTH is required")
		}
		return nil
	}

	if config.SupabaseURL == "" {
		return fmt.Errorf("SUPABASE_URL is required when not using direct connection")
	}
	if config.SupabaseServiceKey == "" {
		return fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY is required when not using direct connection")
	}
	if config.WhatsAppBaseURL == "" {
		return fmt.Errorf("WHATSAPP_BASE_URL is required")
	}
	if config.WhatsAppAuth == "" {
		return fmt.Errorf("WHATSAPP_AUTH is required")
	}

	// Validate multiple endpoints configuration
	if len(config.WhatsAppEndpoints) != len(config.WhatsAppAuths) {
		return fmt.Errorf("WHATSAPP_ENDPOINTS and WHATSAPP_AUTHS must have the same number of entries")
	}

	return nil
}
