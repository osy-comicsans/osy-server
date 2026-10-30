package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv        string
	AppName       string
	LogLevel      string
	HTTPPort      string
	APIKey        string
	HTTPTimeout   time.Duration
	ShutdownGrace time.Duration
	MigrationsDir string
	DatabaseURL   string
	DBMaxConns    int32
	Provider      string
	AfricaTalking AfricaTalking
	Fetcher       Fetcher
	NATS          NATS
	Worker        Worker
	Retry         Retry
	Outbox        Outbox
	Twilio        Twilio
	CallMeBot     CallMeBot
	Notifications Notifications
	WhatsApp      WhatsApp
}

type AfricaTalking struct {
	Username    string
	APIKey      string
	BaseURL     string
	HTTPTimeout time.Duration
}

type Fetcher struct {
	Interval  time.Duration
	BatchSize int
}

type NATS struct {
	URL             string
	SMSStream       string
	SMSSubject      string
	DLQStream       string
	DLQSubject      string
	StreamMaxAge    time.Duration
	DuplicateWindow time.Duration
	StreamMaxMsgs   int64
	StreamMaxBytes  int64
	PublishTimeout  time.Duration
}

type Worker struct {
	Concurrency     int
	MaxDeliveries   int
	AckWait         time.Duration
	FetchWait       time.Duration
	ProcessTimeout  time.Duration
	ConsumerDurable string
}

type Retry struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

type Outbox struct {
	PublishInterval time.Duration
	BatchSize       int
	Lease           time.Duration
}

type Twilio struct {
	AccountSID          string
	AuthToken           string
	FromNumber          string
	MessagingServiceSID string
	BaseURL             string
	HTTPTimeout         time.Duration
	DryRun              bool
}

type Notifications struct {
	Workers              int
	MaxRadiusMeters      int
	CivilProtectionPhone string
	GendarmeriePhone     string
	PolicePhone          string
}

type WhatsApp struct {
	Enabled bool
	To      string
	DryRun  bool
}

type CallMeBot struct {
	APIKey      string
	BaseURL     string
	HTTPTimeout time.Duration
}

func Load() (*Config, error) {
	var errs []error

	duration := func(key string, fallback time.Duration) time.Duration {
		value, err := durationEnv(key, fallback)
		if err != nil {
			errs = append(errs, err)
		}
		return value
	}
	integer := func(key string, fallback int) int {
		value, err := intEnv(key, fallback)
		if err != nil {
			errs = append(errs, err)
		}
		return value
	}
	int64Value := func(key string, fallback int64) int64 {
		value, err := int64Env(key, fallback)
		if err != nil {
			errs = append(errs, err)
		}
		return value
	}

	cfg := &Config{
		AppEnv:        env("APP_ENV", "development"),
		AppName:       env("APP_NAME", "osyn"),
		LogLevel:      env("LOG_LEVEL", "info"),
		HTTPPort:      env("HTTP_PORT", "8080"),
		APIKey:        env("API_KEY", ""),
		HTTPTimeout:   duration("HTTP_TIMEOUT", 15*time.Second),
		ShutdownGrace: duration("SHUTDOWN_GRACE", 20*time.Second),
		MigrationsDir: env("MIGRATIONS_DIR", "db/migrations"),
		DatabaseURL:   env("DATABASE_URL", ""),
		DBMaxConns:    int32(integer("DB_MAX_CONNS", 20)),
		Provider:      env("PROVIDER_NAME", "africastalking"),
		AfricaTalking: AfricaTalking{
			Username:    env("AFRICASTALKING_USERNAME", ""),
			APIKey:      env("AFRICASTALKING_API_KEY", ""),
			BaseURL:     env("AFRICASTALKING_BASE_URL", "https://api.sandbox.africastalking.com"),
			HTTPTimeout: duration("AFRICASTALKING_HTTP_TIMEOUT", 15*time.Second),
		},
		Fetcher: Fetcher{
			Interval:  duration("FETCH_INTERVAL", 10*time.Second),
			BatchSize: integer("FETCH_BATCH_SIZE", 100),
		},
		NATS: NATS{
			URL:             env("NATS_URL", ""),
			SMSStream:       env("NATS_SMS_STREAM", "SMS_MESSAGES"),
			SMSSubject:      env("NATS_SMS_SUBJECT", "sms.incoming"),
			DLQStream:       env("NATS_DLQ_STREAM", "SMS_DLQ"),
			DLQSubject:      env("NATS_DLQ_SUBJECT", "sms.dlq"),
			StreamMaxAge:    duration("NATS_STREAM_MAX_AGE", 168*time.Hour),
			DuplicateWindow: duration("NATS_DUPLICATE_WINDOW", 24*time.Hour),
			StreamMaxMsgs:   int64Value("NATS_STREAM_MAX_MSGS", 1_000_000),
			StreamMaxBytes:  int64Value("NATS_STREAM_MAX_BYTES", 1<<30),
			PublishTimeout:  duration("NATS_PUBLISH_TIMEOUT", 10*time.Second),
		},
		Worker: Worker{
			Concurrency:     integer("WORKER_CONCURRENCY", 5),
			MaxDeliveries:   integer("WORKER_MAX_DELIVERIES", 5),
			AckWait:         duration("WORKER_ACK_WAIT", 30*time.Second),
			FetchWait:       duration("WORKER_FETCH_WAIT", 2*time.Second),
			ProcessTimeout:  duration("WORKER_PROCESS_TIMEOUT", 25*time.Second),
			ConsumerDurable: env("WORKER_CONSUMER_DURABLE", "osyn-worker"),
		},
		Retry: Retry{
			InitialDelay: duration("RETRY_INITIAL_DELAY", 2*time.Second),
			MaxDelay:     duration("RETRY_MAX_DELAY", 5*time.Minute),
			MaxAttempts:  integer("RETRY_MAX_ATTEMPTS", 5),
		},
		Outbox: Outbox{
			PublishInterval: duration("OUTBOX_PUBLISH_INTERVAL", time.Second),
			BatchSize:       integer("OUTBOX_BATCH_SIZE", 100),
			Lease:           duration("OUTBOX_LEASE", 30*time.Second),
		},
		Twilio: Twilio{
			AccountSID:          env("TWILIO_ACCOUNT_SID", ""),
			AuthToken:           env("TWILIO_AUTH_TOKEN", ""),
			FromNumber:          env("TWILIO_FROM_NUMBER", ""),
			MessagingServiceSID: env("TWILIO_MESSAGING_SERVICE_SID", ""),
			BaseURL:             env("TWILIO_BASE_URL", "https://api.twilio.com"),
			HTTPTimeout:         duration("TWILIO_HTTP_TIMEOUT", 15*time.Second),
			DryRun:              boolValue("TWILIO_DRY_RUN", true),
		},
		Notifications: Notifications{
			Workers:              integer("NOTIFICATION_WORKERS", 4),
			MaxRadiusMeters:      integer("ACCIDENT_MAX_RADIUS_METERS", 100000),
			CivilProtectionPhone: env("EMERGENCY_CIVIL_PROTECTION_PHONE", "14"),
			GendarmeriePhone:     env("EMERGENCY_GENDARMERIE_PHONE", "1055"),
			PolicePhone:          env("EMERGENCY_POLICE_PHONE", "1548"),
		},
		WhatsApp: WhatsApp{
			Enabled: boolValue("WHATSAPP_NOTIFICATIONS_ENABLED", false),
			To:      env("OPERATOR_WHATSAPP_NUMBER", ""),
			DryRun:  boolValue("WHATSAPP_DRY_RUN", true),
		},
		CallMeBot: CallMeBot{
			APIKey:      env("CALLMEBOT_API_KEY", ""),
			BaseURL:     env("CALLMEBOT_BASE_URL", "https://api.callmebot.com/whatsapp.php"),
			HTTPTimeout: duration("CALLMEBOT_HTTP_TIMEOUT", 20*time.Second),
		},
	}

	port, err := strconv.Atoi(cfg.HTTPPort)
	if err != nil || port < 1 || port > 65535 {
		errs = append(errs, fmt.Errorf("HTTP_PORT must be a valid port"))
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if cfg.NATS.URL == "" {
		errs = append(errs, errors.New("NATS_URL is required"))
	}
	if cfg.APIKey == "" {
		errs = append(errs, errors.New("API_KEY is required"))
	}
	if cfg.AppEnv == "production" && cfg.APIKey == "change-me" {
		errs = append(errs, errors.New("API_KEY must not use the example value in production"))
	}
	if cfg.DBMaxConns < 1 {
		errs = append(errs, errors.New("DB_MAX_CONNS must be positive"))
	}
	if cfg.Provider == "" {
		errs = append(errs, errors.New("PROVIDER_NAME is required"))
	}
	if cfg.Fetcher.Interval <= 0 {
		errs = append(errs, errors.New("FETCH_INTERVAL must be positive"))
	}
	if cfg.Fetcher.BatchSize < 1 {
		errs = append(errs, errors.New("FETCH_BATCH_SIZE must be positive"))
	}
	if cfg.Worker.Concurrency < 1 {
		errs = append(errs, errors.New("WORKER_CONCURRENCY must be positive"))
	}
	if cfg.Worker.MaxDeliveries < 1 {
		errs = append(errs, errors.New("WORKER_MAX_DELIVERIES must be positive"))
	}
	if cfg.Worker.AckWait <= 0 {
		errs = append(errs, errors.New("WORKER_ACK_WAIT must be positive"))
	}
	if cfg.Worker.FetchWait <= 0 {
		errs = append(errs, errors.New("WORKER_FETCH_WAIT must be positive"))
	}
	if cfg.Retry.InitialDelay <= 0 {
		errs = append(errs, errors.New("RETRY_INITIAL_DELAY must be positive"))
	}
	if cfg.Retry.MaxDelay <= 0 {
		errs = append(errs, errors.New("RETRY_MAX_DELAY must be positive"))
	}
	if cfg.Retry.MaxAttempts < 1 {
		errs = append(errs, errors.New("RETRY_MAX_ATTEMPTS must be positive"))
	}
	if cfg.Outbox.PublishInterval <= 0 {
		errs = append(errs, errors.New("OUTBOX_PUBLISH_INTERVAL must be positive"))
	}
	if cfg.Outbox.BatchSize < 1 {
		errs = append(errs, errors.New("OUTBOX_BATCH_SIZE must be positive"))
	}
	if cfg.Outbox.Lease <= 0 {
		errs = append(errs, errors.New("OUTBOX_LEASE must be positive"))
	}
	if cfg.NATS.SMSStream == "" || cfg.NATS.SMSSubject == "" {
		errs = append(errs, errors.New("NATS SMS stream and subject are required"))
	}
	if cfg.NATS.DLQStream == "" || cfg.NATS.DLQSubject == "" {
		errs = append(errs, errors.New("NATS DLQ stream and subject are required"))
	}
	if cfg.NATS.StreamMaxAge <= 0 || cfg.NATS.DuplicateWindow <= 0 {
		errs = append(errs, errors.New("NATS retention settings must be positive"))
	}
	if cfg.NATS.PublishTimeout <= 0 {
		errs = append(errs, errors.New("NATS_PUBLISH_TIMEOUT must be positive"))
	}
	if cfg.Worker.ProcessTimeout <= 0 {
		errs = append(errs, errors.New("WORKER_PROCESS_TIMEOUT must be positive"))
	}
	if cfg.ShutdownGrace <= 0 {
		errs = append(errs, errors.New("SHUTDOWN_GRACE must be positive"))
	}
	if cfg.Notifications.Workers < 1 {
		errs = append(errs, errors.New("NOTIFICATION_WORKERS must be positive"))
	}
	if cfg.Notifications.MaxRadiusMeters < 1 {
		errs = append(errs, errors.New("ACCIDENT_MAX_RADIUS_METERS must be positive"))
	}
	cfg.validateProvider(&errs)
	cfg.validateLogLevel(&errs)
	cfg.validateURLs(&errs)

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) ValidateProvider() error {
	var errs []error
	c.validateProvider(&errs)
	return errors.Join(errs...)
}

func (c *Config) ValidateNotifications() error {
	var errs []error
	if c.WhatsApp.Enabled {
		if strings.TrimSpace(c.WhatsApp.To) == "" {
			errs = append(errs, errors.New("OPERATOR_WHATSAPP_NUMBER is required when WhatsApp notifications are enabled"))
		} else if !isE164(c.WhatsApp.To) {
			errs = append(errs, errors.New("OPERATOR_WHATSAPP_NUMBER must be E.164"))
		}
		if !c.WhatsApp.DryRun {
			if strings.TrimSpace(c.CallMeBot.APIKey) == "" {
				errs = append(errs, errors.New("CALLMEBOT_API_KEY is required when WhatsApp is live"))
			}
			parsed, err := url.Parse(c.CallMeBot.BaseURL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				errs = append(errs, errors.New("CALLMEBOT_BASE_URL must be a valid http or https URL"))
			}
			if c.CallMeBot.HTTPTimeout <= 0 {
				errs = append(errs, errors.New("CALLMEBOT_HTTP_TIMEOUT must be positive"))
			}
		}
	}
	if strings.TrimSpace(c.Notifications.CivilProtectionPhone) == "" {
		errs = append(errs, errors.New("EMERGENCY_CIVIL_PROTECTION_PHONE is required"))
	}
	if strings.TrimSpace(c.Notifications.GendarmeriePhone) == "" {
		errs = append(errs, errors.New("EMERGENCY_GENDARMERIE_PHONE is required"))
	}
	if strings.TrimSpace(c.Notifications.PolicePhone) == "" {
		errs = append(errs, errors.New("EMERGENCY_POLICE_PHONE is required"))
	}
	if c.Twilio.DryRun {
		return errors.Join(errs...)
	}
	if strings.TrimSpace(c.Twilio.AccountSID) == "" {
		errs = append(errs, errors.New("TWILIO_ACCOUNT_SID is required when TWILIO_DRY_RUN is false"))
	}
	if strings.TrimSpace(c.Twilio.AuthToken) == "" {
		errs = append(errs, errors.New("TWILIO_AUTH_TOKEN is required when TWILIO_DRY_RUN is false"))
	}
	if strings.TrimSpace(c.Twilio.FromNumber) == "" && strings.TrimSpace(c.Twilio.MessagingServiceSID) == "" {
		errs = append(errs, errors.New("TWILIO_FROM_NUMBER or TWILIO_MESSAGING_SERVICE_SID is required when TWILIO_DRY_RUN is false"))
	}
	if c.Twilio.FromNumber != "" && !isE164(c.Twilio.FromNumber) {
		errs = append(errs, errors.New("TWILIO_FROM_NUMBER must be E.164"))
	}
	parsed, err := url.Parse(c.Twilio.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		errs = append(errs, errors.New("TWILIO_BASE_URL must be a valid http or https URL"))
	}
	if c.Twilio.HTTPTimeout <= 0 {
		errs = append(errs, errors.New("TWILIO_HTTP_TIMEOUT must be positive"))
	}
	targets := map[string]string{
		"EMERGENCY_CIVIL_PROTECTION_PHONE": c.Notifications.CivilProtectionPhone,
		"EMERGENCY_GENDARMERIE_PHONE":      c.Notifications.GendarmeriePhone,
		"EMERGENCY_POLICE_PHONE":           c.Notifications.PolicePhone,
	}
	for name, phone := range targets {
		if phone != "" && !isE164(phone) {
			errs = append(errs, fmt.Errorf("%s must be E.164 when TWILIO_DRY_RUN is false", name))
		}
	}
	return errors.Join(errs...)
}

func isE164(phone string) bool {
	if len(phone) < 8 || len(phone) > 16 || phone[0] != '+' || phone[1] == '0' {
		return false
	}
	for _, char := range phone[1:] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func (c *Config) validateProvider(errs *[]error) {
	if strings.TrimSpace(c.AfricaTalking.Username) == "" {
		*errs = append(*errs, errors.New("AFRICASTALKING_USERNAME is required"))
	}
	if strings.TrimSpace(c.AfricaTalking.APIKey) == "" {
		*errs = append(*errs, errors.New("AFRICASTALKING_API_KEY is required"))
	}
	parsed, err := url.Parse(c.AfricaTalking.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		*errs = append(*errs, errors.New("AFRICASTALKING_BASE_URL must be a valid http or https URL"))
	}
	if c.AfricaTalking.HTTPTimeout <= 0 {
		*errs = append(*errs, errors.New("AFRICASTALKING_HTTP_TIMEOUT must be positive"))
	}
}

func (c *Config) validateLogLevel(errs *[]error) {
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		*errs = append(*errs, errors.New("LOG_LEVEL must be debug, info, warn, or error"))
	}
}

func (c *Config) validateURLs(errs *[]error) {
	if c.DatabaseURL != "" {
		parsed, err := url.Parse(c.DatabaseURL)
		if err != nil || parsed.Scheme == "" {
			*errs = append(*errs, errors.New("DATABASE_URL must be a valid URL"))
		}
	}
	if c.NATS.URL != "" {
		parsed, err := url.Parse(c.NATS.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			*errs = append(*errs, errors.New("NATS_URL must be a valid URL"))
		}
	}
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func intEnv(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func int64Env(key string, fallback int64) (int64, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func boolValue(key string, fallback bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
