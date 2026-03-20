package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"x-tool/internal/admin"
	"x-tool/internal/config"
	"x-tool/internal/logging"
	"x-tool/internal/proxy"
	"x-tool/internal/stats"
	"x-tool/internal/storage"
)

func main() {
	env := config.LoadEnv()

	logger, logStore, logLevel, err := buildLogger(env)
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = logStore.Close()
	}()

	dbTarget := env.DBPath
	if strings.EqualFold(env.DBDriver, "postgres") {
		dbTarget = env.DBDSN
	}
	db, err := storage.Open(env.DBDriver, dbTarget)
	if err != nil {
		logger.Error("failed to open database", "driver", env.DBDriver, "target", dbTarget, "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.Migrate(); err != nil {
		logger.Error("failed to migrate database", "error", err)
		os.Exit(1)
	}

	configStore := config.NewConfigStore(db)
	if err := configStore.Seed(defaultConfig(env)); err != nil {
		logger.Error("failed to seed default configuration", "error", err)
		os.Exit(1)
	}

	cfg, err := configStore.LoadAppConfig(env)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("configuration validation failed", "error", err)
		os.Exit(1)
	}
	applyLogLevel(logLevel, cfg.Features.LogLevel)
	if len(cfg.UpstreamServices) > 0 {
		if _, err := cfg.BuildRoutingTable(); err != nil {
			logger.Error("configuration routing validation failed", "error", err)
			os.Exit(1)
		}
	}

	app, err := proxy.NewApp(cfg, configStore, env, logger, logStore, logLevel)
	if err != nil {
		logger.Error("failed to initialize application", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = app.Close()
	}()
	app.SetStatsStore(stats.NewStore(db))

	adminHandler := &admin.Admin{
		GetConfig:  app.Config,
		GetRouting: app.Routing,
		GetStats: func() map[string]any {
			snapshot := app.GetRuntimeStats()
			return map[string]any{
				"total_requests":    snapshot.TotalRequests,
				"inflight_requests": snapshot.InflightRequests,
				"stream_requests":   snapshot.StreamRequests,
				"status_2xx":        snapshot.Status2xx,
				"status_4xx":        snapshot.Status4xx,
				"status_5xx":        snapshot.Status5xx,
				"updated_at":        snapshot.UpdatedAt,
			}
		},
		ConfigStore:      configStore,
		LogStore:         logStore,
		StartedAt:        time.Now().UTC(),
		ReloadConfig:     app.ReloadConfig,
		EnvAdminPassword: env.AdminPassword,
	}

	if available, err := adminHandler.Available(); err != nil {
		logger.Error("failed to inspect admin auth configuration", "error", err)
		os.Exit(1)
	} else if !available {
		logger.Warn("admin interface is unavailable because admin password is not configured")
	}

	addr := cfg.Server.Host + ":" + cfg.Server.PortString()
	logger.Info("starting x-tool", "addr", addr, "timeout_seconds", cfg.Server.Timeout)

	server := &http.Server{
		Addr:    addr,
		Handler: app.Routes(adminHandler),
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func buildLogger(env *config.ServerEnv) (*slog.Logger, *logging.LogStore, *slog.LevelVar, error) {
	if env == nil {
		env = config.LoadEnv()
	}

	levelVar := &slog.LevelVar{}
	levelVar.Set(slog.LevelInfo)

	fileWriter, err := logging.NewBufferedFileWriterWithOptions(env.LogPath, nil, &logging.BufferedFileWriterOptions{
		MaxSizeBytes: int64(env.LogMaxSizeMB) * 1024 * 1024,
		MaxBackups:   env.LogMaxBackups,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open log file %q: %w", env.LogPath, err)
	}
	store := logging.NewLogStore(env.LogPath, fileWriter)

	options := &slog.HandlerOptions{Level: levelVar}
	fileHandler := slog.NewTextHandler(fileWriter, options)
	stdoutHandler := slog.NewTextHandler(os.Stdout, options)
	fanout := logging.NewFanoutHandler(fileHandler, stdoutHandler)

	return slog.New(fanout), store, levelVar, nil
}

func applyLogLevel(levelVar *slog.LevelVar, level string) {
	if levelVar == nil {
		return
	}
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		levelVar.Set(slog.LevelDebug)
	case "WARNING":
		levelVar.Set(slog.LevelWarn)
	case "ERROR", "CRITICAL":
		levelVar.Set(slog.LevelError)
	case "DISABLED":
		levelVar.Set(slog.LevelError + 1)
	default:
		levelVar.Set(slog.LevelInfo)
	}
}

func defaultConfig(_ *config.ServerEnv) *config.AppConfig {
	logLevel := "INFO"

	return &config.AppConfig{
		Features: config.FeaturesConfig{
			EnableFunctionCalling:    true,
			LogLevel:                 logLevel,
			ConvertDeveloperToSystem: true,
			PromptInjectionRole:      "system",
			SoftToolProtocol:         config.SoftToolProtocolXML,
			KeyPassthrough:           false,
			ModelPassthrough:         false,
			PromptTemplate:           "",
		},
	}
}
