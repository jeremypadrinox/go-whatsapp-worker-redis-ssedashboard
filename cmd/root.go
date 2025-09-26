package cmd

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"gowhatsapp-worker/internal/config"
	"gowhatsapp-worker/internal/database"
	"gowhatsapp-worker/internal/redis"
	"gowhatsapp-worker/internal/whatsapp"
	"gowhatsapp-worker/internal/worker"
)

var (
	whatsappClient *whatsapp.Client
	workerService  *worker.Processor
	databaseClient database.DatabaseInterface
	redisClient    *redis.Client
	appConfig      *config.Config

	databaseInitialized bool
	whatsappInitialized bool
	redisInitialized    bool
	workerInitialized   bool
)

var rootCmd = &cobra.Command{
	Use:   "gowhatsapp-worker",
	Short: "High-performance WhatsApp message processing worker",
	Long: `Gowhatsapp-worker is a high-performance Golang implementation designed 
 to process thousands of WhatsApp messages with sequential single-target API processing.

 Features:
 • Sequential message processing
 • Natural human-like behavior simulation
 • Automatic retry with exponential backoff
 • Health monitoring and stuck message recovery
 • Comprehensive logging and statistics`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			cmd.SetArgs([]string{"start"})
			cmd.Execute()
			return
		}
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	loadConfig()
	initFlags()
	cobra.OnInitialize(initSharedComponents)
}

func loadConfig() {
	viper.SetConfigName(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
	}

	viper.SetDefault("SUPABASE_URL", "")
	viper.SetDefault("SUPABASE_SERVICE_ROLE_KEY", "")
	viper.SetDefault("WHATSAPP_BASE_URL", "")
	viper.SetDefault("WHATSAPP_AUTH", "")
	viper.SetDefault("WORKER_INTERVAL", "5")
	viper.SetDefault("MAX_RETRY_ATTEMPTS", "3")
	viper.SetDefault("LOG_LEVEL", "info")

	viper.SetDefault("WORKER_CONCURRENCY", 10)
	viper.SetDefault("WORKER_BATCH_SIZE", 50)
	viper.SetDefault("WORKER_POLL_INTERVAL", "5s")
	viper.SetDefault("WORKER_MAX_RETRIES", 3)
	viper.SetDefault("RATE_LIMIT_MIN_MESSAGES", 6)
	viper.SetDefault("RATE_LIMIT_MAX_MESSAGES", 14)
	viper.SetDefault("RATE_LIMIT_DELAY_MIN", "6s")
	viper.SetDefault("RATE_LIMIT_DELAY_MAX", "12s")
	viper.SetDefault("TYPING_DELAY_MIN", "2s")
	viper.SetDefault("TYPING_DELAY_MAX", "5s")
	viper.SetDefault("NATURAL_BREAK_INTERVAL", 25)
	viper.SetDefault("NATURAL_BREAK_DURATION_MIN", "2m")
	viper.SetDefault("NATURAL_BREAK_DURATION_MAX", "5m")
	viper.SetDefault("HEALTH_CHECK_INTERVAL", "5m")
	viper.SetDefault("STUCK_MESSAGE_TIMEOUT", "5m")

	viper.SetDefault("API_PORT", "8081")
	viper.SetDefault("API_HOST", "0.0.0.0")
	viper.SetDefault("ENABLE_CORS", true)
	viper.SetDefault("WEB_PORT", "3000")
	viper.SetDefault("WEB_HOST", "0.0.0.0")
	viper.SetDefault("ENABLE_WEBSOCKET", true)
	viper.SetDefault("ENABLE_HEALTH_CHECK", true)
	viper.SetDefault("HEALTH_CHECK_INTERVAL_SECONDS", 300)
}

func initFlags() {
	rootCmd.PersistentFlags().StringVarP(
		&config.SupabaseURL,
		"supabase-url",
		"",
		viper.GetString("SUPABASE_URL"),
		"Supabase database URL",
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.SupabaseKey,
		"supabase-key",
		"",
		viper.GetString("SUPABASE_SERVICE_ROLE_KEY"),
		"Supabase API key",
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsAppAPIURL,
		"whatsapp-api-url",
		"",
		viper.GetString("WHATSAPP_BASE_URL"),
		"WhatsApp API URL",
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsAppAPIKey,
		"whatsapp-api-key",
		"",
		viper.GetString("WHATSAPP_AUTH"),
		"WhatsApp API key",
	)
	rootCmd.PersistentFlags().IntVarP(
		&config.WorkerInterval,
		"interval",
		"i",
		viper.GetInt("WORKER_INTERVAL"),
		"Worker processing interval in seconds",
	)
	rootCmd.PersistentFlags().IntVarP(
		&config.MaxRetryAttempts,
		"max-retries",
		"r",
		viper.GetInt("MAX_RETRY_ATTEMPTS"),
		"Maximum retry attempts for failed messages",
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.LogLevel,
		"log-level",
		"l",
		viper.GetString("LOG_LEVEL"),
		"Log level (debug, info, warn, error)",
	)

	viper.BindPFlag("SUPABASE_URL", rootCmd.PersistentFlags().Lookup("supabase-url"))
	viper.BindPFlag("SUPABASE_SERVICE_ROLE_KEY", rootCmd.PersistentFlags().Lookup("supabase-key"))
	viper.BindPFlag("WHATSAPP_BASE_URL", rootCmd.PersistentFlags().Lookup("whatsapp-api-url"))
	viper.BindPFlag("WHATSAPP_AUTH", rootCmd.PersistentFlags().Lookup("whatsapp-api-key"))
	viper.BindPFlag("WORKER_INTERVAL", rootCmd.PersistentFlags().Lookup("interval"))
	viper.BindPFlag("MAX_RETRY_ATTEMPTS", rootCmd.PersistentFlags().Lookup("max-retries"))
	viper.BindPFlag("LOG_LEVEL", rootCmd.PersistentFlags().Lookup("log-level"))
}

func initSharedComponents() {
	var err error
	appConfig, err = config.LoadConfig()
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}

	config.SupabaseURL = appConfig.SupabaseURL
	config.SupabaseKey = appConfig.SupabaseServiceKey
	config.WhatsAppAPIURL = appConfig.WhatsAppBaseURL
	config.WhatsAppAPIKey = appConfig.WhatsAppAuth
	config.WorkerInterval = int(appConfig.WorkerPollInterval.Seconds())
	config.MaxRetryAttempts = appConfig.WorkerMaxRetries
	config.LogLevel = appConfig.LogLevel
	config.EnableHealthCheck = true
	config.HealthCheckInterval = int(appConfig.HealthCheckInterval.Seconds())

	setLogLevel()
	initDatabase()
	initWhatsAppClient()
	initWorkerService()
}

func setLogLevel() {
	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		logrus.Warnf("Invalid log level '%s', using 'info'", config.LogLevel)
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
}

func initDatabase() {
	if databaseInitialized {
		return
	}

	var err error

	if appConfig.DatabaseHost != "" {
		databaseClient, err = database.NewDirectPostgresDatabase(
			appConfig.DatabaseHost,
			appConfig.DatabasePort,
			appConfig.DatabaseName,
			appConfig.DatabaseUser,
			appConfig.DatabasePassword,
			appConfig.DatabaseSSLMode,
		)
		if err != nil {
			logrus.Fatalf("Failed to initialize direct database: %v", err)
		}
		logrus.Info("✅ Database connected")
		databaseInitialized = true
	} else {
		logrus.Fatal("DATABASE_HOST is required for direct connection")
	}
}

func initWhatsAppClient() {
	if whatsappInitialized {
		return
	}

	logger := logrus.StandardLogger()

	whatsappClient = whatsapp.NewClient(config.WhatsAppAPIURL, config.WhatsAppAPIKey, logger)
	logrus.Info("✅ WhatsApp client ready")
	whatsappInitialized = true
}

func initRedisClient() {
	if redisInitialized {
		return
	}

	var err error
	redisClient, err = redis.NewClient(appConfig)
	if err != nil {
		logrus.Fatalf("Failed to initialize Redis client: %v", err)
	}
	logrus.Info("✅ Redis connected using streams")
	redisInitialized = true
}

func initWorkerService() {
	if workerInitialized {
		return
	}

	initRedisClient()

	logger := logrus.StandardLogger()

	workerService = worker.NewProcessor(appConfig, databaseClient, redisClient, whatsappClient, logger)
	logrus.Info("✅ Worker service ready")
	workerInitialized = true
}

func GetSharedComponents() (database.DatabaseInterface, *whatsapp.Client, *worker.Processor) {
	ensureComponentsInitialized()
	return databaseClient, whatsappClient, workerService
}

func GetSharedComponentsWithRedis() (database.DatabaseInterface, *redis.Client, *whatsapp.Client, *worker.Processor) {
	ensureComponentsInitialized()
	return databaseClient, redisClient, whatsappClient, workerService
}

func ensureComponentsInitialized() {
	initDatabase()
	initWhatsAppClient()
	initWorkerService()
}
