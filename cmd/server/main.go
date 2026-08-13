// Package main wires up the Stellar TX Submitter service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gamp/stellar-tx-submitter/internal/api"
	"github.com/gamp/stellar-tx-submitter/internal/channelaccount"
	"github.com/gamp/stellar-tx-submitter/internal/config"
	"github.com/gamp/stellar-tx-submitter/internal/metrics"
	"github.com/gamp/stellar-tx-submitter/internal/retry"
	"github.com/gamp/stellar-tx-submitter/internal/state"
	"github.com/gamp/stellar-tx-submitter/internal/submitter"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stellar/go-stellar-sdk/clients/horizonclient"
)

func bootstrapChannelPool(ctx context.Context, cfg config.Config, pool *channelaccount.Pool, horizon *horizonclient.Client, logger *slog.Logger) (int, error) {
	// Load channel account secrets from environment
	secretSource := channelaccount.NewEnvSecretSource("CHANNEL_ACCOUNT_SECRETS")
	secrets, err := secretSource.ChannelAccountSecrets(ctx)
	if err != nil {
		return 0, fmt.Errorf("loading channel account secrets: %w", err)
	}

	// Seed the pool with the secrets (idempotent upsert)
	if err := pool.InitPool(ctx, secrets); err != nil {
		return 0, fmt.Errorf("seeding channel account pool: %w", err)
	}

	// Provision additional accounts on-chain if pool is below target
	provisioner, err := channelaccount.NewProvisioner(horizon, cfg.NetworkPassphrase, cfg.DistributionAccountSecret)
	if err != nil {
		return 0, fmt.Errorf("creating channel account provisioner: %w", err)
	}

	if err := provisioner.EnsurePoolSize(ctx, pool, cfg.NumChannelAccounts, cfg.ChannelAccountStartingBalance); err != nil {
		logger.Warn("provisioning additional channel accounts failed; continuing with seeded pool", "error", err)
	}

	// Sync each account's current_seq from its real on-chain sequence so the
	// first submission uses the correct sequence number instead of a stale 0.
	if err := provisioner.SyncSequences(ctx, pool, logger); err != nil {
		logger.Warn("syncing channel account sequences failed; some accounts may retain a stale sequence", "error", err)
	}

	// Return final pool size
	size, err := pool.PoolSize(ctx)
	if err != nil {
		return 0, fmt.Errorf("checking channel account pool size: %w", err)
	}
	return size, nil
}

func main() {
	cfg := config.Load()

	// Setup structured logging
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Connect to Postgres
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Log(context.Background(), slog.LevelError, "failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Create state store
	store := state.New(pool)

	// Create channel account pool
	poolAccount := channelaccount.New(store)

	// Create retry policy
	policy := retry.NewPolicy(
		cfg.RetryMaxAttempts,
		100,   // base fee in stroops
		cfg.MaxBaseFee,
		2.0,   // 2x multiplier
		10*time.Second,
		cfg.HorizonURL,
	)

	// Create Horizon client
	horizon := &horizonclient.Client{
		HorizonURL: cfg.HorizonURL,
	}

	// Bootstrap channel account pool
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 60*time.Second)
	poolSize, err := bootstrapChannelPool(startupCtx, cfg, poolAccount, horizon, logger)
	startupCancel()
	if err != nil {
		logger.Error("channel account pool bootstrap failed", "error", err)
		os.Exit(1)
	}
	if poolSize == 0 {
		logger.Error("channel account pool is empty after seeding and provisioning; refusing to start with zero channel accounts")
		os.Exit(1)
	}
	metrics.SetActiveChannels(float64(poolSize))
	logger.Info("channel account pool ready", "size", poolSize, "target", cfg.NumChannelAccounts)

	// Create transaction builder
	builder := submitter.NewBuilder(cfg.NetworkPassphrase, 100, cfg.MaxBaseFee)

	// Create engine
	engine, err := submitter.NewEngine(
		horizon,
		store,
		poolAccount,
		builder,
		policy,
		cfg.NetworkPassphrase,
		cfg.DistributionAccountSecret,
		cfg.MaxBaseFee,
		cfg.QueuePollingInterval,
		logger,
	)
	if err != nil {
		logger.Log(context.Background(), slog.LevelError, "failed to create engine", "error", err)
		os.Exit(1)
	}

	// Create API handlers
	handlers := api.NewHandlers(store)
	router := api.Router(handlers)

	// Setup HTTP server
	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Setup metrics endpoint
	metricsSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.MetricsPort),
		Handler:      http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			promhttp.Handler().ServeHTTP(w, r)
		})),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start metrics server in background
	go func() {
		logger.Info("starting metrics server", "port", cfg.MetricsPort)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log(ctx, slog.LevelError, "metrics server error", "error", err)
		}
	}()

	// Start engine in background
	go func() {
		logger.Info("starting submission engine")
		engine.Start(ctx)
	}()

	// Start HTTP server
	go func() {
		logger.Info("starting HTTP server", "port", cfg.HTTPPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log(ctx, slog.LevelError, "HTTP server error", "error", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	logger.Info("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	httpSrv.Shutdown(shutdownCtx)
	metricsSrv.Shutdown(shutdownCtx)

	logger.Info("shutdown complete")
}
