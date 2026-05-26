package room

import (
	"errors"
	"sync"
)

// ErrRoomNotFound is returned when a room ID is unknown.
var ErrRoomNotFound = errors.New("room: not found")

// Manager owns every active room in process memory.
type Manager struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

// NewManager builds an empty manager.
func NewManager() *Manager {
	return &Manager{rooms: make(map[string]*Room)}
}

// Create allocates a new room and returns it.
func (m *Manager) Create(name string) *Room {
	r := newRoom(name)
	m.mu.Lock()
	m.rooms[r.ID] = r
	m.mu.Unlock()
	return r
}

// Get returns a room by ID.
func (m *Manager) Get(id string) (*Room, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[id]
	if !ok {
		return nil, ErrRoomNotFound
	}
	return r, nil
}

// List returns a snapshot of all rooms.
func (m *Manager) List() []*Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, r)
	}
	return out
}

// Delete removes a room by ID. It is a no-op for unknown rooms.
func (m *Manager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, id)
}

// SweepEmpty deletes rooms that have no participants. Useful from a janitor.
func (m *Manager) SweepEmpty() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, r := range m.rooms {
		if r.Empty() {
			delete(m.rooms, id)
			n++
		}
	}
	return n
}
