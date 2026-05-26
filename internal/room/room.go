package room

import (
	"errors"
	"sync"
	"time"
)

// ErrParticipantNotFound is returned when a participant ID is unknown.
var ErrParticipantNotFound = errors.New("room: participant not found")

// Room is a video-call session. It owns its participants, their tracks,
// and the fan-out for control events.
type Room struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`

	mu           sync.RWMutex
	participants map[string]*Participant
	eventSubs    map[string]chan Event // participant ID -> event channel
}

func newRoom(name string) *Room {
	return &Room{
		ID:           newID(6),
		Name:         name,
		CreatedAt:    time.Now().UTC(),
		participants: make(map[string]*Participant),
		eventSubs:    make(map[string]chan Event),
	}
}

// Join adds a new participant and broadcasts the join event.
func (r *Room) Join(name string) *Participant {
	p := newParticipant(name)
	r.mu.Lock()
	r.participants[p.ID] = p
	r.mu.Unlock()
	r.broadcast(makeEvent(EventParticipantJoined, r.ID, p.ID, p))
	return p
}

// Leave removes a participant, closes their tracks, and notifies the room.
func (r *Room) Leave(pid string) error {
	r.mu.Lock()
	p, ok := r.participants[pid]
	if ok {
		delete(r.participants, pid)
	}
	ch, hadCh := r.eventSubs[pid]
	if hadCh {
		delete(r.eventSubs, pid)
	}
	r.mu.Unlock()
	if !ok {
		return ErrParticipantNotFound
	}
	p.closeAll()
	if hadCh {
		close(ch)
	}
	r.broadcast(makeEvent(EventParticipantLeft, r.ID, pid, map[string]string{"id": pid, "name": p.Name}))
	return nil
}

// Participant returns a participant by ID.
func (r *Room) Participant(pid string) (*Participant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.participants[pid]
	return p, ok
}

// Participants returns a snapshot list of all current participants.
func (r *Room) Participants() []*Participant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Participant, 0, len(r.participants))
	for _, p := range r.participants {
		out = append(out, p)
	}
	return out
}

// AnnounceTrackStarted notifies the room that a track has begun publishing.
func (r *Room) AnnounceTrackStarted(pid string, kind TrackKind, contentType string) {
	r.broadcast(makeEvent(EventTrackStarted, r.ID, pid, map[string]string{
		"participant": pid,
		"kind":        string(kind),
		"contentType": contentType,
	}))
}

// AnnounceTrackEnded notifies the room that a track has stopped publishing.
func (r *Room) AnnounceTrackEnded(pid string, kind TrackKind) {
	r.broadcast(makeEvent(EventTrackEnded, r.ID, pid, map[string]string{
		"participant": pid,
		"kind":        string(kind),
	}))
}

// SubscribeEvents returns an event channel for the participant. The channel
// is closed when the participant leaves or unsubscribes.
func (r *Room) SubscribeEvents(pid string) (<-chan Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.participants[pid]; !ok {
		return nil, ErrParticipantNotFound
	}
	if _, exists := r.eventSubs[pid]; exists {
		return nil, errors.New("room: event subscription already active for participant")
	}
	ch := make(chan Event, 32)
	r.eventSubs[pid] = ch
	ch <- makeEvent(EventHello, r.ID, "", map[string]any{
		"you":          pid,
		"participants": r.snapshotLocked(),
	})
	return ch, nil
}

// UnsubscribeEvents closes the event channel for the participant.
func (r *Room) UnsubscribeEvents(pid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.eventSubs[pid]; ok {
		delete(r.eventSubs, pid)
		close(ch)
	}
}

// snapshotLocked returns participant summaries; caller must hold r.mu.
func (r *Room) snapshotLocked() []map[string]any {
	out := make([]map[string]any, 0, len(r.participants))
	for _, p := range r.participants {
		out = append(out, map[string]any{
			"id":           p.ID,
			"name":         p.Name,
			"joinedAt":     p.JoinedAt,
			"activeTracks": p.ActiveTracks(),
		})
	}
	return out
}

// broadcast fans out an event to every subscriber. Slow subscribers see
// their event dropped rather than blocking the room.
func (r *Room) broadcast(ev Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ch := range r.eventSubs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Empty reports whether the room currently has no participants.
func (r *Room) Empty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.participants) == 0
}
