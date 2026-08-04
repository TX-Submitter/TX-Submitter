// Package config loads and validates environment variables for the Stellar TX Submitter.
// It fails fast: any missing required variable causes a fatal error at startup.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration derived from environment variables.
type Config struct {
	HorizonURL                    string
	NetworkPassphrase             string
	DistributionAccountSecret     string
	NumChannelAccounts            int
	DatabaseURL                   string
	MaxBaseFee                    int64
	RetryMaxAttempts              int
	QueuePollingInterval          time.Duration
	HTTPPort                      int
	MetricsPort                   int
	LogLevel                      string
}

// loadRequired reads a required env var and exits if missing.
func loadRequired(key string) string {
	val := os.Getenv(key)
	if val == "" {
		panic(fmt.Sprintf("required env var %q is not set", key))
	}
	return val
}

// loadInt reads an env var as int, returns defaultValue if empty.
func loadInt(key string, defaultValue int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		panic(fmt.Sprintf("env var %q must be an integer: %v", key, err))
	}
	return n
}

// loadInt64 reads an env var as int64, returns defaultValue if empty.
func loadInt64(key string, defaultValue int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("env var %q must be an integer: %v", key, err))
	}
	return n
}

// Load reads all environment variables and returns a validated Config.
// It panics on any missing required variable or invalid value.
func Load() Config {
	return Config{
		HorizonURL:                    loadRequired("HORIZON_URL"),
		NetworkPassphrase:             loadRequired("NETWORK_PASSPHRASE"),
		DistributionAccountSecret:     loadRequired("DISTRIBUTION_ACCOUNT_SECRET"),
		NumChannelAccounts:            loadInt("NUM_CHANNEL_ACCOUNTS", 2),
		DatabaseURL:                   loadRequired("DATABASE_URL"),
		MaxBaseFee:                    loadInt64("MAX_BASE_FEE", 10000),
		RetryMaxAttempts:              loadInt("RETRY_MAX_ATTEMPTS", 5),
		QueuePollingInterval:          time.Duration(loadInt("QUEUE_POLLING_INTERVAL_SECONDS", 6)) * time.Second,
		HTTPPort:                      loadInt("HTTP_PORT", 8080),
		MetricsPort:                   loadInt("METRICS_PORT", 9002),
		LogLevel:                      loadInt("LOG_LEVEL", "INFO"),
	}
}
