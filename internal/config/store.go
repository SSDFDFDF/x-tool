package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SQLDBProvider interface {
	SqlDB() *sql.DB
	Rebind(string) string
}

type ConfigStore struct {
	db SQLDBProvider
}

func NewConfigStore(db SQLDBProvider) *ConfigStore {
	return &ConfigStore{db: db}
}

func (s *ConfigStore) ListUpstreams() ([]UpstreamService, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return nil, err
	}

	rows, err := s.query(sqlDB, `
SELECT name, base_url, api_key, models, client_keys, description, prompt_injection_role, prompt_injection_target, soft_tool_calling_protocol, soft_tool_prompt_profile_id, soft_tool_retry_attempts, upstream_protocol, is_default
FROM upstream_services
ORDER BY name
`)
	if err != nil {
		return nil, fmt.Errorf("list upstream services: %w", err)
	}
	defer rows.Close()

	var result []UpstreamService
	for rows.Next() {
		var svc UpstreamService
		var modelsJSON string
		var clientKeysJSON string
		var isDefault int
		if err := rows.Scan(
			&svc.Name,
			&svc.BaseURL,
			&svc.APIKey,
			&modelsJSON,
			&clientKeysJSON,
			&svc.Description,
			&svc.PromptInjectionRole,
			&svc.PromptInjectionTarget,
			&svc.SoftToolProtocol,
			&svc.SoftToolPromptProfileID,
			&svc.SoftToolRetryAttempts,
			&svc.UpstreamProtocol,
			&isDefault,
		); err != nil {
			return nil, fmt.Errorf("scan upstream service: %w", err)
		}
		models, err := decodeModels(modelsJSON)
		if err != nil {
			return nil, fmt.Errorf("decode models for %q: %w", svc.Name, err)
		}
		clientKeys, err := decodeClientKeys(clientKeysJSON)
		if err != nil {
			return nil, fmt.Errorf("decode client keys for %q: %w", svc.Name, err)
		}
		svc.Models = models
		svc.ClientKeys = clientKeys
		svc.IsDefault = isDefault != 0
		result = append(result, svc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream services: %w", err)
	}
	return result, nil
}

func (s *ConfigStore) GetUpstream(name string) (*UpstreamService, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return nil, err
	}

	row := s.queryRow(sqlDB, `
SELECT name, base_url, api_key, models, client_keys, description, prompt_injection_role, prompt_injection_target, soft_tool_calling_protocol, soft_tool_prompt_profile_id, soft_tool_retry_attempts, upstream_protocol, is_default
FROM upstream_services
WHERE name = ?
`, name)

	var svc UpstreamService
	var modelsJSON string
	var clientKeysJSON string
	var isDefault int
	if err := row.Scan(
		&svc.Name,
		&svc.BaseURL,
		&svc.APIKey,
		&modelsJSON,
		&clientKeysJSON,
		&svc.Description,
		&svc.PromptInjectionRole,
		&svc.PromptInjectionTarget,
		&svc.SoftToolProtocol,
		&svc.SoftToolPromptProfileID,
		&svc.SoftToolRetryAttempts,
		&svc.UpstreamProtocol,
		&isDefault,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("upstream service %q not found: %w", name, err)
		}
		return nil, fmt.Errorf("get upstream service %q: %w", name, err)
	}
	models, err := decodeModels(modelsJSON)
	if err != nil {
		return nil, fmt.Errorf("decode models for %q: %w", svc.Name, err)
	}
	clientKeys, err := decodeClientKeys(clientKeysJSON)
	if err != nil {
		return nil, fmt.Errorf("decode client keys for %q: %w", svc.Name, err)
	}
	svc.Models = models
	svc.ClientKeys = clientKeys
	svc.IsDefault = isDefault != 0
	return &svc, nil
}

func (s *ConfigStore) SaveUpstream(svc *UpstreamService) error {
	if svc == nil {
		return fmt.Errorf("upstream service is nil")
	}
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	modelsJSON, err := encodeModels(svc.Models)
	if err != nil {
		return fmt.Errorf("encode models for %q: %w", svc.Name, err)
	}
	clientKeysJSON, err := encodeClientKeys(svc.ClientKeys)
	if err != nil {
		return fmt.Errorf("encode client keys for %q: %w", svc.Name, err)
	}
	isDefault := 0
	if svc.IsDefault {
		isDefault = 1
	}

	_, err = s.exec(sqlDB, `
INSERT INTO upstream_services (
    name, base_url, api_key, models, client_keys, description, prompt_injection_role, prompt_injection_target, soft_tool_calling_protocol, soft_tool_prompt_profile_id, soft_tool_retry_attempts, upstream_protocol, is_default, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET
    base_url = excluded.base_url,
    api_key = excluded.api_key,
    models = excluded.models,
    client_keys = excluded.client_keys,
    description = excluded.description,
    prompt_injection_role = excluded.prompt_injection_role,
    prompt_injection_target = excluded.prompt_injection_target,
    soft_tool_calling_protocol = excluded.soft_tool_calling_protocol,
    soft_tool_prompt_profile_id = excluded.soft_tool_prompt_profile_id,
    soft_tool_retry_attempts = excluded.soft_tool_retry_attempts,
    upstream_protocol = excluded.upstream_protocol,
    is_default = excluded.is_default,
    updated_at = CURRENT_TIMESTAMP
`,
		svc.Name,
		svc.BaseURL,
		svc.APIKey,
		modelsJSON,
		clientKeysJSON,
		svc.Description,
		svc.PromptInjectionRole,
		svc.PromptInjectionTarget,
		svc.SoftToolProtocol,
		svc.SoftToolPromptProfileID,
		svc.SoftToolRetryAttempts,
		svc.UpstreamProtocol,
		isDefault,
	)
	if err != nil {
		return fmt.Errorf("save upstream service %q: %w", svc.Name, err)
	}
	return nil
}

func (s *ConfigStore) DeleteUpstream(name string) error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	if _, err := s.exec(sqlDB, `DELETE FROM upstream_services WHERE name = ?`, name); err != nil {
		return fmt.Errorf("delete upstream service %q: %w", name, err)
	}
	return nil
}

func (s *ConfigStore) ListClientKeys() ([]string, error) {
	upstreams, err := s.ListUpstreams()
	if err != nil {
		return nil, err
	}
	cfg := &AppConfig{UpstreamServices: upstreams}
	return cfg.ClientKeys(), nil
}

func (s *ConfigStore) ListSoftToolPromptProfiles() ([]SoftToolPromptProfile, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return nil, err
	}

	rows, err := s.query(sqlDB, `
SELECT id, name, description, protocol, template, enabled
FROM soft_tool_prompt_profiles
ORDER BY name, id
`)
	if err != nil {
		return nil, fmt.Errorf("list soft tool prompt profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]SoftToolPromptProfile, 0)
	for rows.Next() {
		var profile SoftToolPromptProfile
		var enabled int
		if err := rows.Scan(
			&profile.ID,
			&profile.Name,
			&profile.Description,
			&profile.Protocol,
			&profile.Template,
			&enabled,
		); err != nil {
			return nil, fmt.Errorf("scan soft tool prompt profile: %w", err)
		}
		profile.Enabled = enabled != 0
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate soft tool prompt profiles: %w", err)
	}
	return profiles, nil
}

func (s *ConfigStore) SaveSoftToolPromptProfile(profile *SoftToolPromptProfile) error {
	if profile == nil {
		return fmt.Errorf("soft tool prompt profile is nil")
	}
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	enabled := 0
	if profile.Enabled {
		enabled = 1
	}
	_, err = s.exec(sqlDB, `
INSERT INTO soft_tool_prompt_profiles (
    id, name, description, protocol, template, enabled, updated_at
) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    description = excluded.description,
    protocol = excluded.protocol,
    template = excluded.template,
    enabled = excluded.enabled,
    updated_at = CURRENT_TIMESTAMP
`,
		profile.ID,
		profile.Name,
		profile.Description,
		profile.Protocol,
		profile.Template,
		enabled,
	)
	if err != nil {
		return fmt.Errorf("save soft tool prompt profile %q: %w", profile.ID, err)
	}
	return nil
}

func (s *ConfigStore) GetFeature(key string) (string, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return "", err
	}

	var value string
	if err := s.queryRow(sqlDB, `SELECT value FROM features WHERE key = ?`, key).Scan(&value); err != nil {
		return "", fmt.Errorf("get feature %q: %w", key, err)
	}
	return value, nil
}

func (s *ConfigStore) SetFeature(key, value string) error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}
	if _, err := s.exec(sqlDB, `
INSERT INTO features (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
`, key, value); err != nil {
		return fmt.Errorf("set feature %q: %w", key, err)
	}
	return nil
}

func (s *ConfigStore) GetAllFeatures() (map[string]string, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return nil, err
	}

	rows, err := s.query(sqlDB, `SELECT key, value FROM features`)
	if err != nil {
		return nil, fmt.Errorf("list features: %w", err)
	}
	defer rows.Close()

	values := make(map[string]string)
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan feature: %w", err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate features: %w", err)
	}
	return values, nil
}

func (s *ConfigStore) GetAdminPasswordHash() (string, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return "", err
	}

	var value string
	if err := s.queryRow(sqlDB, `SELECT value FROM features WHERE key = ?`, featureAdminPasswordHash).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get admin password hash: %w", err)
	}
	return strings.TrimSpace(value), nil
}

func (s *ConfigStore) SetAdminPasswordHash(hash string) error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	if _, err := s.exec(sqlDB, `
INSERT INTO features (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
`, featureAdminPasswordHash, strings.TrimSpace(hash)); err != nil {
		return fmt.Errorf("set admin password hash: %w", err)
	}
	return nil
}

func (s *ConfigStore) CreateAdminSession(tokenHash string, expiresAt, now time.Time) error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	if _, err := s.exec(sqlDB, `
INSERT INTO admin_sessions (token_hash, expires_at, created_at, last_seen_at)
VALUES (?, ?, ?, ?)
`, strings.TrimSpace(tokenHash), expiresAt.UTC(), now.UTC(), now.UTC()); err != nil {
		return fmt.Errorf("create admin session: %w", err)
	}
	return nil
}

func (s *ConfigStore) TouchAdminSession(tokenHash string, now time.Time) (bool, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return false, err
	}

	result, err := s.exec(sqlDB, `
UPDATE admin_sessions
SET last_seen_at = ?
WHERE token_hash = ? AND expires_at > ?
`, now.UTC(), strings.TrimSpace(tokenHash), now.UTC())
	if err != nil {
		return false, fmt.Errorf("touch admin session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("touch admin session rows affected: %w", err)
	}
	return affected > 0, nil
}

func (s *ConfigStore) DeleteAdminSession(tokenHash string) error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	if _, err := s.exec(sqlDB, `DELETE FROM admin_sessions WHERE token_hash = ?`, strings.TrimSpace(tokenHash)); err != nil {
		return fmt.Errorf("delete admin session: %w", err)
	}
	return nil
}

func (s *ConfigStore) DeleteExpiredAdminSessions(now time.Time) error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	if _, err := s.exec(sqlDB, `DELETE FROM admin_sessions WHERE expires_at <= ?`, now.UTC()); err != nil {
		return fmt.Errorf("delete expired admin sessions: %w", err)
	}
	return nil
}

func (s *ConfigStore) DeleteAllAdminSessions() error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	if _, err := s.exec(sqlDB, `DELETE FROM admin_sessions`); err != nil {
		return fmt.Errorf("delete all admin sessions: %w", err)
	}
	return nil
}

func (s *ConfigStore) LoadAppConfig(env *ServerEnv) (*AppConfig, error) {
	upstreams, err := s.ListUpstreams()
	if err != nil {
		return nil, err
	}
	promptProfiles, err := s.ListSoftToolPromptProfiles()
	if err != nil {
		return nil, err
	}

	features, err := s.GetAllFeatures()
	if err != nil {
		return nil, err
	}

	cfg := &AppConfig{
		UpstreamServices:       upstreams,
		SoftToolPromptProfiles: promptProfiles,
	}

	applyFeatures(&cfg.Features, features)
	EnsureBuiltInSoftToolPromptProfiles(cfg)
	cfg.ApplyServerEnv(env)
	cfg.applyDefaults()
	return cfg, nil
}

func (s *ConfigStore) SaveAppConfig(cfg *AppConfig) error {
	if cfg == nil {
		return fmt.Errorf("app config is nil")
	}
	EnsureBuiltInSoftToolPromptProfiles(cfg)
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	adminPasswordHash, err := s.GetAdminPasswordHash()
	if err != nil {
		return err
	}

	tx, err := sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("begin config save transaction: %w", err)
	}

	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	if _, err := s.txExec(tx, `DELETE FROM upstream_services`); err != nil {
		return rollback(fmt.Errorf("clear upstream services: %w", err))
	}
	if _, err := s.txExec(tx, `DELETE FROM soft_tool_prompt_profiles`); err != nil {
		return rollback(fmt.Errorf("clear soft tool prompt profiles: %w", err))
	}
	if _, err := s.txExec(tx, `DELETE FROM features`); err != nil {
		return rollback(fmt.Errorf("clear features: %w", err))
	}

	for _, svc := range cfg.UpstreamServices {
		modelsJSON, err := encodeModels(svc.Models)
		if err != nil {
			return rollback(fmt.Errorf("encode models for %q: %w", svc.Name, err))
		}
		clientKeysJSON, err := encodeClientKeys(svc.ClientKeys)
		if err != nil {
			return rollback(fmt.Errorf("encode client keys for %q: %w", svc.Name, err))
		}
		isDefault := 0
		if svc.IsDefault {
			isDefault = 1
		}
		if _, err := s.txExec(tx, `
INSERT INTO upstream_services (
    name, base_url, api_key, models, client_keys, description, prompt_injection_role, prompt_injection_target, soft_tool_calling_protocol, soft_tool_prompt_profile_id, soft_tool_retry_attempts, upstream_protocol, is_default, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
`,
			svc.Name,
			svc.BaseURL,
			svc.APIKey,
			modelsJSON,
			clientKeysJSON,
			svc.Description,
			svc.PromptInjectionRole,
			svc.PromptInjectionTarget,
			svc.SoftToolProtocol,
			svc.SoftToolPromptProfileID,
			svc.SoftToolRetryAttempts,
			svc.UpstreamProtocol,
			isDefault,
		); err != nil {
			return rollback(fmt.Errorf("insert upstream service %q: %w", svc.Name, err))
		}
	}

	for _, profile := range cfg.SoftToolPromptProfiles {
		enabled := 0
		if profile.Enabled {
			enabled = 1
		}
		if _, err := s.txExec(tx, `
INSERT INTO soft_tool_prompt_profiles (
    id, name, description, protocol, template, enabled, updated_at
) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
`,
			profile.ID,
			profile.Name,
			profile.Description,
			profile.Protocol,
			profile.Template,
			enabled,
		); err != nil {
			return rollback(fmt.Errorf("insert soft tool prompt profile %q: %w", profile.ID, err))
		}
	}

	features := featuresFromConfig(cfg.Features)
	for key, value := range features {
		if _, err := s.txExec(tx, `INSERT INTO features (key, value) VALUES (?, ?)`, key, value); err != nil {
			return rollback(fmt.Errorf("insert feature %q: %w", key, err))
		}
	}
	if strings.TrimSpace(adminPasswordHash) != "" {
		if _, err := s.txExec(tx, `INSERT INTO features (key, value) VALUES (?, ?)`, featureAdminPasswordHash, adminPasswordHash); err != nil {
			return rollback(fmt.Errorf("insert feature %q: %w", featureAdminPasswordHash, err))
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit config save transaction: %w", err)
	}
	return nil
}

func (s *ConfigStore) Seed(defaults *AppConfig) error {
	if defaults == nil {
		return fmt.Errorf("defaults config is nil")
	}
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	var count int
	if err := s.queryRow(sqlDB, `SELECT COUNT(*) FROM upstream_services`).Scan(&count); err != nil {
		return fmt.Errorf("count upstream services: %w", err)
	}
	if count == 0 {
		for i := range defaults.UpstreamServices {
			if err := s.SaveUpstream(&defaults.UpstreamServices[i]); err != nil {
				return err
			}
		}
	}

	if err := s.queryRow(sqlDB, `SELECT COUNT(*) FROM soft_tool_prompt_profiles`).Scan(&count); err != nil {
		return fmt.Errorf("count soft tool prompt profiles: %w", err)
	}
	if count == 0 {
		for i := range defaults.SoftToolPromptProfiles {
			if err := s.SaveSoftToolPromptProfile(&defaults.SoftToolPromptProfiles[i]); err != nil {
				return err
			}
		}
	}

	if err := s.queryRow(sqlDB, `SELECT COUNT(*) FROM features`).Scan(&count); err != nil {
		return fmt.Errorf("count features: %w", err)
	}
	if count == 0 {
		features := featuresFromConfig(defaults.Features)
		for key, value := range features {
			if err := s.SetFeature(key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ConfigStore) sqlDB() (*sql.DB, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("storage database is not initialized")
	}
	sqlDB := s.db.SqlDB()
	if sqlDB == nil {
		return nil, fmt.Errorf("storage database is not initialized")
	}
	return sqlDB, nil
}

func (s *ConfigStore) rebind(query string) string {
	if s == nil || s.db == nil {
		return query
	}
	return s.db.Rebind(query)
}

func (s *ConfigStore) exec(db *sql.DB, query string, args ...any) (sql.Result, error) {
	return db.Exec(s.rebind(query), args...)
}

func (s *ConfigStore) query(db *sql.DB, query string, args ...any) (*sql.Rows, error) {
	return db.Query(s.rebind(query), args...)
}

func (s *ConfigStore) queryRow(db *sql.DB, query string, args ...any) *sql.Row {
	return db.QueryRow(s.rebind(query), args...)
}

func (s *ConfigStore) txExec(tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return tx.Exec(s.rebind(query), args...)
}

func encodeModels(models []string) (string, error) {
	if models == nil {
		models = []string{}
	}
	payload, err := json.Marshal(models)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeModels(payload string) ([]string, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return []string{}, nil
	}
	var models []string
	if err := json.Unmarshal([]byte(payload), &models); err != nil {
		return nil, err
	}
	return models, nil
}

func encodeClientKeys(keys []string) (string, error) {
	return encodeModels(keys)
}

func decodeClientKeys(payload string) ([]string, error) {
	return decodeModels(payload)
}

const (
	featureEnableFunctionCalling          = "enable_function_calling"
	featureLogLevel                       = "log_level"
	featureConvertDeveloperToSystem       = "convert_developer_to_system"
	featurePromptTemplate                 = "prompt_template"
	featureDefaultSoftToolPromptProfileID = "default_soft_tool_prompt_profile_id"
	featurePromptInjectionRole            = "prompt_injection_role"
	featurePromptInjectionTarget          = "prompt_injection_target"
	featureSoftToolProtocol               = "soft_tool_calling_protocol"
	featureSoftToolRetryAttempts          = "soft_tool_retry_attempts"
	featureKeyPassthrough                 = "key_passthrough"
	featureModelPassthrough               = "model_passthrough"
	featureAdminPasswordHash              = "admin_password_hash"
)

func applyFeatures(cfg *FeaturesConfig, values map[string]string) {
	if cfg == nil {
		return
	}
	cfg.EnableFunctionCalling = parseBool(values[featureEnableFunctionCalling])
	cfg.LogLevel = strings.TrimSpace(values[featureLogLevel])
	cfg.ConvertDeveloperToSystem = parseBool(values[featureConvertDeveloperToSystem])
	cfg.PromptTemplate = strings.TrimSpace(values[featurePromptTemplate])
	cfg.DefaultSoftToolPromptProfileID = strings.TrimSpace(values[featureDefaultSoftToolPromptProfileID])
	cfg.PromptInjectionRole = strings.TrimSpace(values[featurePromptInjectionRole])
	cfg.PromptInjectionTarget = strings.TrimSpace(values[featurePromptInjectionTarget])
	cfg.SoftToolProtocol = strings.TrimSpace(values[featureSoftToolProtocol])
	cfg.SoftToolRetryAttempts = parseInt(values[featureSoftToolRetryAttempts])
	cfg.KeyPassthrough = parseBool(values[featureKeyPassthrough])
	cfg.ModelPassthrough = parseBool(values[featureModelPassthrough])
}

func featuresFromConfig(cfg FeaturesConfig) map[string]string {
	return map[string]string{
		featureEnableFunctionCalling:          formatBool(cfg.EnableFunctionCalling),
		featureLogLevel:                       strings.TrimSpace(cfg.LogLevel),
		featureConvertDeveloperToSystem:       formatBool(cfg.ConvertDeveloperToSystem),
		featurePromptTemplate:                 strings.TrimSpace(cfg.PromptTemplate),
		featureDefaultSoftToolPromptProfileID: strings.TrimSpace(cfg.DefaultSoftToolPromptProfileID),
		featurePromptInjectionRole:            strings.TrimSpace(cfg.PromptInjectionRole),
		featurePromptInjectionTarget:          strings.TrimSpace(cfg.PromptInjectionTarget),
		featureSoftToolProtocol:               strings.TrimSpace(cfg.SoftToolProtocol),
		featureSoftToolRetryAttempts:          strconv.Itoa(cfg.SoftToolRetryAttempts),
		featureKeyPassthrough:                 formatBool(cfg.KeyPassthrough),
		featureModelPassthrough:               formatBool(cfg.ModelPassthrough),
	}
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseInt(value string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n >= 0 {
		return n
	}
	return 0
}

func formatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
