package room

import (
	"errors"
	"sync"
	"time"
)

// Participant is a member of a room. Each participant owns up to one audio
// and one video track at a time.
type Participant struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	JoinedAt time.Time `json:"joinedAt"`

	mu     sync.RWMutex
	tracks map[TrackKind]*Track
}

func newParticipant(name string) *Participant {
	return &Participant{
		ID:       newID(8),
		Name:     name,
		JoinedAt: time.Now().UTC(),
		tracks:   make(map[TrackKind]*Track),
	}
}

// StartTrack registers a new publishing track. It fails if a track of the
// same kind is already live.
func (p *Participant) StartTrack(kind TrackKind, contentType string) (*Track, error) {
	if !kind.Valid() {
		return nil, errors.New("room: invalid track kind")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.tracks[kind]; ok && !existing.Closed() {
		return nil, errors.New("room: track already active")
	}
	t := newTrack(kind, contentType)
	p.tracks[kind] = t
	return t, nil
}

// EndTrack closes the named track if it is active.
func (p *Participant) EndTrack(kind TrackKind) {
	p.mu.Lock()
	t, ok := p.tracks[kind]
	if ok {
		delete(p.tracks, kind)
	}
	p.mu.Unlock()
	if ok {
		t.Close()
	}
}

// Track returns the live track of the given kind, if any.
func (p *Participant) Track(kind TrackKind) (*Track, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	t, ok := p.tracks[kind]
	if !ok || t.Closed() {
		return nil, false
	}
	return t, true
}

// ActiveTracks returns the kinds of tracks currently being published.
func (p *Participant) ActiveTracks() []TrackKind {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]TrackKind, 0, len(p.tracks))
	for k, t := range p.tracks {
		if !t.Closed() {
			out = append(out, k)
		}
	}
	return out
}

// closeAll ends every track owned by the participant; called on leave.
func (p *Participant) closeAll() {
	p.mu.Lock()
	tracks := make([]*Track, 0, len(p.tracks))
	for _, t := range p.tracks {
		tracks = append(tracks, t)
	}
	p.tracks = make(map[TrackKind]*Track)
	p.mu.Unlock()
	for _, t := range tracks {
		t.Close()
	}
}
