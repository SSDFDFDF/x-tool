package proxy

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"x-tool/internal/stats"
)

const statsFlushInterval = 10 * time.Second

type runtimeStats struct {
	totalRequests    atomic.Uint64
	inflightRequests atomic.Uint64
	streamRequests   atomic.Uint64
	status2xx        atomic.Uint64
	status4xx        atomic.Uint64
	status5xx        atomic.Uint64
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := r.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := r.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (a *App) GetRuntimeStats() stats.Snapshot {
	if a == nil {
		return stats.Snapshot{}
	}

	snapshot := stats.Snapshot{
		TotalRequests:    a.runtimeStats.totalRequests.Load(),
		InflightRequests: a.runtimeStats.inflightRequests.Load(),
		StreamRequests:   a.runtimeStats.streamRequests.Load(),
		Status2xx:        a.runtimeStats.status2xx.Load(),
		Status4xx:        a.runtimeStats.status4xx.Load(),
		Status5xx:        a.runtimeStats.status5xx.Load(),
	}

	if a.statsStore != nil {
		if persisted, err := a.statsStore.Load(stats.GlobalScope); err == nil && persisted.UpdatedAt != "" {
			snapshot.UpdatedAt = persisted.UpdatedAt
		}
	}

	return snapshot
}

func (a *App) SetStatsStore(store *stats.Store) {
	if a == nil {
		return
	}

	a.statsMu.Lock()
	defer a.statsMu.Unlock()

	if a.statsCancel != nil {
		a.statsCancel()
		a.statsCancel = nil
	}

	a.statsStore = store
	if store == nil {
		return
	}

	if snapshot, err := store.Load(stats.GlobalScope); err == nil {
		a.runtimeStats.totalRequests.Store(snapshot.TotalRequests)
		a.runtimeStats.streamRequests.Store(snapshot.StreamRequests)
		a.runtimeStats.status2xx.Store(snapshot.Status2xx)
		a.runtimeStats.status4xx.Store(snapshot.Status4xx)
		a.runtimeStats.status5xx.Store(snapshot.Status5xx)
	} else if a.logger != nil {
		a.logger.Warn("failed to load runtime stats", "error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.statsCancel = cancel
	go a.runStatsFlusher(ctx)
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}

	a.statsMu.Lock()
	cancel := a.statsCancel
	a.statsCancel = nil
	a.statsMu.Unlock()

	if cancel != nil {
		cancel()
	}
	a.flushStatsNow()
	return nil
}

func (a *App) statsMiddleware(next http.Handler) http.Handler {
	if next == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.runtimeStats.totalRequests.Add(1)
		a.runtimeStats.inflightRequests.Add(1)

		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		defer func() {
			a.runtimeStats.inflightRequests.Add(^uint64(0))
			switch {
			case rec.statusCode >= 500:
				a.runtimeStats.status5xx.Add(1)
			case rec.statusCode >= 400:
				a.runtimeStats.status4xx.Add(1)
			case rec.statusCode >= 200:
				a.runtimeStats.status2xx.Add(1)
			}
		}()

		next.ServeHTTP(rec, r)
	})
}

func (a *App) recordStreamRequest() {
	a.runtimeStats.streamRequests.Add(1)
}

func (a *App) runStatsFlusher(ctx context.Context) {
	ticker := time.NewTicker(statsFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.flushStatsNow()
		}
	}
}

func (a *App) flushStatsNow() {
	if a == nil || a.statsStore == nil {
		return
	}

	a.statsFlushMu.Lock()
	defer a.statsFlushMu.Unlock()

	snapshot := stats.Snapshot{
		TotalRequests:  a.runtimeStats.totalRequests.Load(),
		StreamRequests: a.runtimeStats.streamRequests.Load(),
		Status2xx:      a.runtimeStats.status2xx.Load(),
		Status4xx:      a.runtimeStats.status4xx.Load(),
		Status5xx:      a.runtimeStats.status5xx.Load(),
	}

	if err := a.statsStore.Save(stats.GlobalScope, snapshot); err != nil && a.logger != nil {
		a.logger.Warn("failed to flush runtime stats", "error", err)
	}
}
