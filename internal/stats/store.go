package stats

import (
	"database/sql"
	"errors"
	"fmt"
)

const GlobalScope = "global"

type SQLDBProvider interface {
	SqlDB() *sql.DB
	Rebind(string) string
}

type Snapshot struct {
	TotalRequests    uint64 `json:"total_requests"`
	InflightRequests uint64 `json:"inflight_requests"`
	StreamRequests   uint64 `json:"stream_requests"`
	Status2xx        uint64 `json:"status_2xx"`
	Status4xx        uint64 `json:"status_4xx"`
	Status5xx        uint64 `json:"status_5xx"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type Store struct {
	db SQLDBProvider
}

func NewStore(db SQLDBProvider) *Store {
	return &Store{db: db}
}

func (s *Store) Load(scope string) (Snapshot, error) {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return Snapshot{}, err
	}

	row := sqlDB.QueryRow(s.db.Rebind(`
SELECT total_requests, stream_requests, status_2xx, status_4xx, status_5xx, updated_at
FROM runtime_stats
WHERE scope = ?
`), scope)

	var snapshot Snapshot
	if err := row.Scan(
		&snapshot.TotalRequests,
		&snapshot.StreamRequests,
		&snapshot.Status2xx,
		&snapshot.Status4xx,
		&snapshot.Status5xx,
		&snapshot.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Snapshot{}, nil
		}
		return Snapshot{}, fmt.Errorf("load runtime stats: %w", err)
	}
	return snapshot, nil
}

func (s *Store) Save(scope string, snapshot Snapshot) error {
	sqlDB, err := s.sqlDB()
	if err != nil {
		return err
	}

	_, err = sqlDB.Exec(s.db.Rebind(`
INSERT INTO runtime_stats (
    scope, total_requests, stream_requests, status_2xx, status_4xx, status_5xx, updated_at
) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(scope) DO UPDATE SET
    total_requests = excluded.total_requests,
    stream_requests = excluded.stream_requests,
    status_2xx = excluded.status_2xx,
    status_4xx = excluded.status_4xx,
    status_5xx = excluded.status_5xx,
    updated_at = CURRENT_TIMESTAMP
`),
		scope,
		snapshot.TotalRequests,
		snapshot.StreamRequests,
		snapshot.Status2xx,
		snapshot.Status4xx,
		snapshot.Status5xx,
	)
	if err != nil {
		return fmt.Errorf("save runtime stats: %w", err)
	}
	return nil
}

func (s *Store) sqlDB() (*sql.DB, error) {
	if s == nil || s.db == nil || s.db.SqlDB() == nil {
		return nil, fmt.Errorf("stats store database is not initialized")
	}
	return s.db.SqlDB(), nil
}
