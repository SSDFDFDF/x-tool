package toolcall

import (
	"container/list"
	"sync"
	"time"
)

type Entry struct {
	ID          string
	Name        string
	Args        map[string]any
	Description string
	CreatedAt   time.Time
}

type Manager struct {
	maxSize         int
	ttl             time.Duration
	cleanupInterval time.Duration

	mu    sync.RWMutex
	cache map[string]*list.Element
	order *list.List
}

func NewManager(maxSize int, ttl, cleanupInterval time.Duration) *Manager {
	m := &Manager{
		maxSize:         maxSize,
		ttl:             ttl,
		cleanupInterval: cleanupInterval,
		cache:           make(map[string]*list.Element),
		order:           list.New(),
	}
	go m.cleanupLoop()
	return m
}

func (m *Manager) Put(id, name string, args map[string]any, description string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if elem, ok := m.cache[id]; ok {
		m.order.Remove(elem)
		delete(m.cache, id)
	}

	for len(m.cache) >= m.maxSize {
		oldest := m.order.Front()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*Entry)
		delete(m.cache, entry.ID)
		m.order.Remove(oldest)
	}

	entry := &Entry{
		ID:          id,
		Name:        name,
		Args:        args,
		Description: description,
		CreatedAt:   time.Now(),
	}
	m.cache[id] = m.order.PushBack(entry)
}

func (m *Manager) Get(id string) (*Entry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	elem, ok := m.cache[id]
	if !ok {
		return nil, false
	}

	entry := elem.Value.(*Entry)
	if time.Since(entry.CreatedAt) > m.ttl {
		delete(m.cache, id)
		m.order.Remove(elem)
		return nil, false
	}

	m.order.MoveToBack(elem)
	return entry, true
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanupExpired()
	}
}

func (m *Manager) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for elem := m.order.Front(); elem != nil; {
		next := elem.Next()
		entry := elem.Value.(*Entry)
		if now.Sub(entry.CreatedAt) > m.ttl {
			delete(m.cache, entry.ID)
			m.order.Remove(elem)
		}
		elem = next
	}
}
