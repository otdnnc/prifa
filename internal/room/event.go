package room

import (
	"encoding/json"
	"time"
)

// EventType enumerates room events delivered over the event stream.
type EventType string

const (
	EventParticipantJoined EventType = "participant.joined"
	EventParticipantLeft   EventType = "participant.left"
	EventTrackStarted      EventType = "track.started"
	EventTrackEnded        EventType = "track.ended"
	EventHello             EventType = "hello"
)

// Event is the envelope broadcast to every subscriber of a room.
type Event struct {
	Type      EventType       `json:"type"`
	Room      string          `json:"room"`
	From      string          `json:"from,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func makeEvent(t EventType, roomID, from string, data any) Event {
	ev := Event{
		Type:      t,
		Room:      roomID,
		From:      from,
		Timestamp: time.Now().UTC(),
	}
	if data != nil {
		if b, err := json.Marshal(data); err == nil {
			ev.Data = b
		}
	}
	return ev
}
