package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"x-tool/internal/admin"
	"x-tool/internal/config"
	"x-tool/internal/logging"
	"x-tool/internal/stats"
	"x-tool/internal/toolcall"
)

type AppState struct {
	Cfg     *config.AppConfig
	Routing *config.RoutingTable
}

type App struct {
	state               atomic.Pointer[AppState]
	logger              *slog.Logger
	logStore            *logging.LogStore
	configStore         *config.ConfigStore
	env                 *config.ServerEnv
	client              *http.Client
	trigger             string
	store               *toolcall.Manager
	startedAt           time.Time
	logLevel            *slog.LevelVar
	runtimeStats        runtimeStats
	statsStore          *stats.Store
	statsMu             sync.Mutex
	statsFlushMu        sync.Mutex
	statsCancel         context.CancelFunc
	maxRequestBodyBytes int64
}

var (
	errNoUpstreamConfigured = errors.New("no upstream services configured")
	errModelNotAccessible   = errors.New("requested model is not accessible for this client key")
)

func (a *App) Config() *config.AppConfig {
	return a.state.Load().Cfg
}

func (a *App) Routing() *config.RoutingTable {
	return a.state.Load().Routing
}

func (a *App) MaxRequestBodyBytes() int64 {
	return a.maxRequestBodyBytes
}

func (a *App) ReplaceState(cfg *config.AppConfig, routing *config.RoutingTable) {
	a.state.Store(&AppState{
		Cfg:     cfg,
		Routing: routing,
	})
}

func NewApp(cfg *config.AppConfig, configStore *config.ConfigStore, env *config.ServerEnv, logger *slog.Logger, logStore *logging.LogStore, logLevel *slog.LevelVar) (*App, error) {
	if logStore == nil {
		logStore = logging.NewLogStore("", nil)
	}

	routing, err := buildRoutingTableOrEmpty(cfg)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout: cfg.Server.TimeoutDuration(),
			}).DialContext,
			ResponseHeaderTimeout: cfg.Server.TimeoutDuration(),
			TLSHandshakeTimeout:   cfg.Server.TimeoutDuration(),
			ExpectContinueTimeout: time.Second,
		},
	}

	app := &App{
		logger:              logger,
		logStore:            logStore,
		configStore:         configStore,
		env:                 env,
		client:              client,
		trigger:             GenerateRandomTriggerSignal(),
		store:               toolcall.NewManager(1000, time.Hour, 5*time.Minute),
		startedAt:           time.Now().UTC(),
		logLevel:            logLevel,
		maxRequestBodyBytes: maxRequestBodyBytes(env),
	}
	app.ReplaceState(cfg, routing)
	app.applyLogLevel(cfg.Features.LogLevel)
	return app, nil
}

func maxRequestBodyBytes(env *config.ServerEnv) int64 {
	if env == nil || env.MaxRequestBodyMB <= 0 {
		return 10 * 1024 * 1024
	}
	return int64(env.MaxRequestBodyMB) * 1024 * 1024
}

func (a *App) ReloadConfig() error {
	if a.configStore == nil {
		return errors.New("config store is not initialized")
	}
	cfg, err := a.configStore.LoadAppConfig(a.env)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	routing, err := buildRoutingTableOrEmpty(cfg)
	if err != nil {
		return err
	}
	a.ReplaceState(cfg, routing)
	a.applyLogLevel(cfg.Features.LogLevel)
	return nil
}

func (a *App) applyLogLevel(level string) {
	if a == nil || a.logLevel == nil {
		return
	}
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		a.logLevel.Set(slog.LevelDebug)
	case "WARNING":
		a.logLevel.Set(slog.LevelWarn)
	case "ERROR", "CRITICAL":
		a.logLevel.Set(slog.LevelError)
	case "DISABLED":
		a.logLevel.Set(slog.LevelError + 1)
	default:
		a.logLevel.Set(slog.LevelInfo)
	}
}

func (a *App) Routes(adminHandler *admin.Admin) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleRoot)
	if adminHandler != nil {
		adminHandler.RegisterRoutes(mux)
		mux.Handle("/admin/", http.StripPrefix("/admin/", admin.FileServer()))
	}
	mux.HandleFunc("/v1/models", a.handleModels)
	mux.HandleFunc("/v1/chat/completions", a.handleChatCompletions)
	mux.HandleFunc("/v1/responses", a.handleResponses)
	mux.HandleFunc("/v1/messages", a.handleAnthropicMessages)
	return a.recoverMiddleware(a.statsMiddleware(mux))
}
