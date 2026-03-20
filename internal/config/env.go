package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

type ServerEnv struct {
	Port              int
	Host              string
	Timeout           int
	LogPath           string
	LogMaxSizeMB      int
	LogMaxBackups     int
	DBDriver          string
	DBDSN             string
	DBPath            string
	AdminPassword     string
	MaxRequestBodyMB  int
}

var loadDotEnvOnce sync.Once

func LoadEnv() *ServerEnv {
	loadDotEnvOnce.Do(loadDotEnv)
	return &ServerEnv{
		Port:             readIntEnv([]string{"X_TOOL_PORT", "PORT"}, 8026),
		Host:             readStringEnv([]string{"X_TOOL_HOST", "HOST"}, "0.0.0.0"),
		Timeout:          readIntEnv([]string{"X_TOOL_TIMEOUT"}, 180),
		LogPath:          readStringEnv([]string{"X_TOOL_LOG_PATH"}, "logs/app.log"),
		LogMaxSizeMB:     readIntEnv([]string{"X_TOOL_LOG_MAX_SIZE_MB"}, 100),
		LogMaxBackups:    readIntEnv([]string{"X_TOOL_LOG_MAX_BACKUPS"}, 5),
		DBDriver:         strings.ToLower(readStringEnv([]string{"X_TOOL_DB_DRIVER"}, "sqlite")),
		DBDSN:            readStringEnv([]string{"X_TOOL_DB_DSN"}, ""),
		DBPath:           readStringEnv([]string{"X_TOOL_DB_PATH"}, "data.db"),
		AdminPassword:    readStringEnv([]string{"X_TOOL_ADMIN_PASSWORD"}, ""),
		MaxRequestBodyMB: readIntEnv([]string{"X_TOOL_MAX_REQUEST_BODY_MB"}, 50),
	}
}

func loadDotEnv() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	for {
		candidate := filepath.Join(cwd, ".env")
		if _, err := os.Stat(candidate); err == nil {
			_ = godotenv.Load(candidate)
			return
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return
		}
		cwd = parent
	}
}

func readStringEnv(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func readIntEnv(keys []string, fallback int) int {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
		return fallback
	}
	return fallback
}
