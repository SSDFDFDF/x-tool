package storage

import (
	"fmt"
	"strings"
)

type Migration struct {
	Version     int
	Description string
	Up          map[string]string
}

var migrations = []Migration{
	{
		Version:     1,
		Description: "create upstream services and features tables",
		Up: map[string]string{
			"sqlite": `
CREATE TABLE upstream_services (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    base_url TEXT NOT NULL,
    api_key TEXT NOT NULL DEFAULT '',
    models TEXT NOT NULL DEFAULT '[]',
    client_keys TEXT NOT NULL DEFAULT '[]',
    description TEXT NOT NULL DEFAULT '',
    prompt_injection_role TEXT NOT NULL DEFAULT '',
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE features (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`,
			"postgres": `
CREATE TABLE upstream_services (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    base_url TEXT NOT NULL,
    api_key TEXT NOT NULL DEFAULT '',
    models TEXT NOT NULL DEFAULT '[]',
    client_keys TEXT NOT NULL DEFAULT '[]',
    description TEXT NOT NULL DEFAULT '',
    prompt_injection_role TEXT NOT NULL DEFAULT '',
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE features (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
		},
	},
	{
		Version:     2,
		Description: "add admin sessions table",
		Up: map[string]string{
			"sqlite": `
CREATE TABLE admin_sessions (
    token_hash TEXT PRIMARY KEY,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_admin_sessions_expires_at ON admin_sessions (expires_at);
`,
			"postgres": `
CREATE TABLE admin_sessions (
    token_hash TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_admin_sessions_expires_at ON admin_sessions (expires_at);
`,
		},
	},
	{
		Version:     3,
		Description: "add runtime stats table",
		Up: map[string]string{
			"sqlite": `
CREATE TABLE runtime_stats (
    scope TEXT PRIMARY KEY,
    total_requests INTEGER NOT NULL DEFAULT 0,
    stream_requests INTEGER NOT NULL DEFAULT 0,
    status_2xx INTEGER NOT NULL DEFAULT 0,
    status_4xx INTEGER NOT NULL DEFAULT 0,
    status_5xx INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
			"postgres": `
CREATE TABLE runtime_stats (
    scope TEXT PRIMARY KEY,
    total_requests BIGINT NOT NULL DEFAULT 0,
    stream_requests BIGINT NOT NULL DEFAULT 0,
    status_2xx BIGINT NOT NULL DEFAULT 0,
    status_4xx BIGINT NOT NULL DEFAULT 0,
    status_5xx BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
		},
	},
	{
		Version:     4,
		Description: "add soft tool protocol settings",
		Up: map[string]string{
			"sqlite": `
ALTER TABLE upstream_services ADD COLUMN soft_tool_calling_protocol TEXT NOT NULL DEFAULT '';
`,
			"postgres": `
ALTER TABLE upstream_services ADD COLUMN soft_tool_calling_protocol TEXT NOT NULL DEFAULT '';
`,
		},
	},
	{
		Version:     5,
		Description: "add upstream protocol settings",
		Up: map[string]string{
			"sqlite": `
ALTER TABLE upstream_services ADD COLUMN upstream_protocol TEXT NOT NULL DEFAULT 'openai_compat';
`,
			"postgres": `
ALTER TABLE upstream_services ADD COLUMN upstream_protocol TEXT NOT NULL DEFAULT 'openai_compat';
`,
		},
	},
	{
		Version:     6,
		Description: "add prompt injection target settings",
		Up: map[string]string{
			"sqlite": `
ALTER TABLE upstream_services ADD COLUMN prompt_injection_target TEXT NOT NULL DEFAULT 'auto';
`,
			"postgres": `
ALTER TABLE upstream_services ADD COLUMN prompt_injection_target TEXT NOT NULL DEFAULT 'auto';
`,
		},
	},
	{
		Version:     7,
		Description: "add soft tool prompt profiles",
		Up: map[string]string{
			"sqlite": `
ALTER TABLE upstream_services ADD COLUMN soft_tool_prompt_profile_id TEXT NOT NULL DEFAULT '';

CREATE TABLE soft_tool_prompt_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    protocol TEXT NOT NULL DEFAULT '',
    template TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
			"postgres": `
ALTER TABLE upstream_services ADD COLUMN soft_tool_prompt_profile_id TEXT NOT NULL DEFAULT '';

CREATE TABLE soft_tool_prompt_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    protocol TEXT NOT NULL DEFAULT '',
    template TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
		},
	},
}

func (db *DB) Migrate() (err error) {
	if db == nil || db.db == nil {
		return fmt.Errorf("database is not initialized")
	}

	tx, err := db.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	createSchemaMigrations, err := schemaMigrationsDDL(db.Driver())
	if err != nil {
		return err
	}
	if _, err = tx.Exec(createSchemaMigrations); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	rows, err := tx.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]struct{}, len(migrations))
	for rows.Next() {
		var version int
		if err = rows.Scan(&version); err != nil {
			return fmt.Errorf("scan applied migration version: %w", err)
		}
		applied[version] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate applied migrations: %w", err)
	}

	for _, migration := range migrations {
		if _, ok := applied[migration.Version]; ok {
			continue
		}

		sqlText, err := migrationSQL(migration, db.Driver())
		if err != nil {
			return err
		}
		if strings.TrimSpace(sqlText) == "" {
			return fmt.Errorf("migration %d has empty SQL", migration.Version)
		}

		if _, err = tx.Exec(sqlText); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", migration.Version, migration.Description, err)
		}

		if _, err = tx.Exec(
			db.Rebind(`INSERT INTO schema_migrations (version, description) VALUES (?, ?)`),
			migration.Version,
			migration.Description,
		); err != nil {
			return fmt.Errorf("record migration %d: %w", migration.Version, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	return nil
}

func schemaMigrationsDDL(driver string) (string, error) {
	switch driver {
	case "sqlite":
		return `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`, nil
	case "postgres":
		return `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`, nil
	default:
		return "", fmt.Errorf("unsupported database driver %q", driver)
	}
}

func migrationSQL(m Migration, driver string) (string, error) {
	sqlText, ok := m.Up[driver]
	if !ok {
		return "", fmt.Errorf("migration %d missing %s SQL", m.Version, driver)
	}
	return sqlText, nil
}
