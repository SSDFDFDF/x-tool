package storage

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type DB struct {
	db     *sql.DB
	driver string
}

func Open(primary string, conn ...string) (*DB, error) {
	driver := "sqlite"
	dsn := strings.TrimSpace(primary)

	if len(conn) > 0 {
		driver = strings.ToLower(strings.TrimSpace(primary))
		dsn = strings.TrimSpace(conn[0])
	}

	switch driver {
	case "sqlite":
		if dsn == "" {
			return nil, fmt.Errorf("sqlite path cannot be empty")
		}
		sqlDB, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("open sqlite database %q: %w", dsn, err)
		}
		db := &DB{db: sqlDB, driver: driver}
		if err := db.configure(); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		return db, nil
	case "postgres":
		if dsn == "" {
			return nil, fmt.Errorf("postgres dsn cannot be empty")
		}
		sqlDB, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, fmt.Errorf("open postgres database: %w", err)
		}
		db := &DB{db: sqlDB, driver: driver}
		if err := db.configure(); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		return db, nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
}

func (db *DB) configure() error {
	if db == nil || db.db == nil {
		return fmt.Errorf("database is not initialized")
	}
	if db.driver != "sqlite" {
		return nil
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
	}

	for _, stmt := range pragmas {
		if _, err := db.db.Exec(stmt); err != nil {
			return fmt.Errorf("configure sqlite pragma %q: %w", stmt, err)
		}
	}

	return nil
}

func (db *DB) Close() error {
	if db == nil || db.db == nil {
		return nil
	}
	return db.db.Close()
}

func (db *DB) SqlDB() *sql.DB {
	if db == nil {
		return nil
	}
	return db.db
}

func (db *DB) Driver() string {
	if db == nil || strings.TrimSpace(db.driver) == "" {
		return "sqlite"
	}
	return db.driver
}

func (db *DB) Rebind(query string) string {
	if db == nil || db.Driver() != "postgres" {
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 8)

	argIndex := 1
	inSingle := false
	inDouble := false

	for i := 0; i < len(query); i++ {
		ch := query[i]

		if ch == '\'' && !inDouble {
			if inSingle && i+1 < len(query) && query[i+1] == '\'' {
				b.WriteByte(ch)
				i++
				b.WriteByte(query[i])
				continue
			}
			inSingle = !inSingle
			b.WriteByte(ch)
			continue
		}

		if ch == '"' && !inSingle {
			inDouble = !inDouble
			b.WriteByte(ch)
			continue
		}

		if ch == '?' && !inSingle && !inDouble {
			b.WriteString(fmt.Sprintf("$%d", argIndex))
			argIndex++
			continue
		}

		b.WriteByte(ch)
	}

	return b.String()
}
