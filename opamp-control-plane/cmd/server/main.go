// Package main is the entry point for the OpAMP control plane server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bcrisp4/opamp-control-plane/internal/api"
	"github.com/bcrisp4/opamp-control-plane/internal/config"
	"github.com/bcrisp4/opamp-control-plane/internal/gitsync"
	"github.com/bcrisp4/opamp-control-plane/internal/opamp"
	"github.com/bcrisp4/opamp-control-plane/internal/registry"
	"github.com/bcrisp4/opamp-control-plane/pkg/models"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Config represents the server configuration.
type Config struct {
	Server struct {
		HTTPAddr  string `yaml:"http_addr"`
		OpAMPAddr string `yaml:"opamp_addr"`
	} `yaml:"server"`

	Storage struct {
		Type   string `yaml:"type"`
		SQLite struct {
			Path string `yaml:"path"`
		} `yaml:"sqlite"`
	} `yaml:"storage"`

	Git struct {
		RepoURL       string        `yaml:"repo_url"`
		Branch        string        `yaml:"branch"`
		PollInterval  time.Duration `yaml:"poll_interval"`
		LocalPath     string        `yaml:"local_path"`
		WebhookSecret string        `yaml:"webhook_secret"`
		Username      string        `yaml:"username"`
		Password      string        `yaml:"password"`
		SSHKeyPath    string        `yaml:"ssh_key_path"`
	} `yaml:"git"`

	Validation struct {
		Enabled         bool `yaml:"enabled"`
		StrictOTelSchema bool `yaml:"strict_otel_schema"`
	} `yaml:"validation"`

	Logging struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"logging"`
}

func main() {
	configPath := flag.String("config", "configs/server.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("opamp-control-plane version %s\n", Version)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Setup logger
	logger := setupLogger(cfg.Logging.Level, cfg.Logging.Format)

	logger.Info("starting OpAMP control plane",
		"version", Version,
		"http_addr", cfg.Server.HTTPAddr,
		"opamp_addr", cfg.Server.OpAMPAddr,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize registry
	reg, err := registry.NewSQLiteRegistry(cfg.Storage.SQLite.Path, logger)
	if err != nil {
		logger.Error("failed to create registry", "error", err)
		os.Exit(1)
	}
	defer reg.Close()

	// Initialize config validator
	var validator config.Validator
	if cfg.Validation.Enabled {
		validator = config.NewValidator(cfg.Validation.StrictOTelSchema)
	}

	// Initialize config resolver
	resolver := config.NewResolver(config.ResolverConfig{
		ConfigDir: cfg.Git.LocalPath,
		Validator: validator,
		Logger:    logger,
	})

	// Create config provider function
	configProvider := func(agent *models.Agent) (*models.EffectiveConfig, error) {
		return resolver.Resolve(agent)
	}

	// Initialize OpAMP server first (needed for sync callback)
	opampServer, err := opamp.NewServer(opamp.ServerConfig{
		ListenEndpoint: cfg.Server.OpAMPAddr,
		Registry:       reg,
		ConfigProvider: configProvider,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("failed to create OpAMP server", "error", err)
		os.Exit(1)
	}

	// Initialize Git syncer
	var syncer *gitsync.Syncer
	if cfg.Git.RepoURL != "" {
		syncer, err = gitsync.NewSyncer(gitsync.SyncerConfig{
			RepoURL:      cfg.Git.RepoURL,
			Branch:       cfg.Git.Branch,
			LocalPath:    cfg.Git.LocalPath,
			PollInterval: cfg.Git.PollInterval,
			Username:     cfg.Git.Username,
			Password:     cfg.Git.Password,
			SSHKeyPath:   cfg.Git.SSHKeyPath,
			Logger:       logger,
		})
		if err != nil {
			logger.Error("failed to create git syncer", "error", err)
			os.Exit(1)
		}

		// Register callback to reload configs on sync
		syncer.OnSync(func(commit string) error {
			logger.Info("reloading configs after sync", "commit", commit)
			if err := resolver.LoadConfigs(); err != nil {
				logger.Error("failed to reload configs", "error", err)
				return err
			}
			// Push to all connected agents
			opampServer.PushConfigToAll(ctx)
			return nil
		})

		if err := syncer.Start(ctx); err != nil {
			logger.Error("failed to start git syncer", "error", err)
			os.Exit(1)
		}
	} else {
		// Load configs from local path
		if err := resolver.LoadConfigs(); err != nil {
			logger.Warn("failed to load configs", "error", err)
		}
	}

	// Initialize API handlers
	handlers := &api.Handlers{
		Registry:       reg,
		ConfigResolver: resolver,
		GitSyncer:      syncer,
		Logger:         logger,
		StartTime:      time.Now(),
	}

	// Create HTTP mux
	mux := http.NewServeMux()

	// Mount API routes
	apiRouter := api.NewRouter(handlers)
	mux.Handle("/", api.WithCORS(api.WithLogging(apiRouter, logger)))

	// Mount OpAMP endpoint
	mux.Handle("/v1/opamp", opampServer.Handler())

	// Mount webhook endpoint
	if syncer != nil && cfg.Git.WebhookSecret != "" {
		webhookHandler := gitsync.NewWebhookHandler(syncer, cfg.Git.WebhookSecret, logger)
		mux.Handle("POST /webhook/git", webhookHandler)
		logger.Info("git webhook endpoint enabled", "path", "/webhook/git")
	}

	// Start HTTP server
	httpServer := &http.Server{
		Addr:    cfg.Server.HTTPAddr,
		Handler: mux,
	}

	go func() {
		logger.Info("starting HTTP server", "addr", cfg.Server.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", "error", err)
			cancel()
		}
	}()

	// Start OpAMP server
	go func() {
		logger.Info("starting OpAMP server", "addr", cfg.Server.OpAMPAddr)
		if err := opampServer.Start(ctx, cfg.Server.OpAMPAddr); err != nil {
			logger.Error("OpAMP server error", "error", err)
			cancel()
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("received shutdown signal", "signal", sig)
	case <-ctx.Done():
	}

	// Graceful shutdown
	logger.Info("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}

	if err := opampServer.Stop(shutdownCtx); err != nil {
		logger.Error("OpAMP server shutdown error", "error", err)
	}

	if syncer != nil {
		syncer.Stop()
	}

	logger.Info("shutdown complete")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// Return defaults if config file doesn't exist
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	cfg := defaultConfig()
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func defaultConfig() *Config {
	cfg := &Config{}
	cfg.Server.HTTPAddr = ":8080"
	cfg.Server.OpAMPAddr = ":4320"
	cfg.Storage.Type = "sqlite"
	cfg.Storage.SQLite.Path = "./data/opamp.db"
	cfg.Git.Branch = "main"
	cfg.Git.LocalPath = "./data/configs"
	cfg.Git.PollInterval = 60 * time.Second
	cfg.Validation.Enabled = true
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "text"
	return cfg
}

func setupLogger(level, format string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: logLevel}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
