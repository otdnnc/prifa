package room

import (
	"errors"
	"sync"
)

// TrackKind identifies a media stream from a participant.
type TrackKind string

const (
	TrackAudio TrackKind = "audio"
	TrackVideo TrackKind = "video"
)

func (k TrackKind) Valid() bool {
	return k == TrackAudio || k == TrackVideo
}

// ErrTrackClosed is returned when publishing to a closed track.
var ErrTrackClosed = errors.New("room: track closed")

// Track is a single-publisher, multi-subscriber byte stream. Subscribers
// receive copies of each chunk through a bounded channel; if a subscriber
// falls behind, chunks are dropped for that subscriber only.
//
// The very first chunk a publisher sends is cached as the "init segment"
// and replayed to every subscriber on Subscribe — for fragmented WebM the
// first MediaRecorder chunk carries the EBML/Segment headers that the
// browser's MediaSource needs before it can decode any cluster.
type Track struct {
	kind        TrackKind
	contentType string

	mu          sync.Mutex
	subscribers map[string]chan []byte
	closed      bool
	initSegment []byte
}

func newTrack(kind TrackKind, contentType string) *Track {
	return &Track{
		kind:        kind,
		contentType: contentType,
		subscribers: make(map[string]chan []byte),
	}
}

// Kind returns the track's kind.
func (t *Track) Kind() TrackKind { return t.kind }

// ContentType returns the MIME type the publisher declared.
func (t *Track) ContentType() string { return t.contentType }

// Subscribe registers a subscriber identified by id. The returned channel
// is closed when the track ends or when Unsubscribe is called. If the
// publisher has already sent its first chunk, that chunk is queued on the
// new subscriber's channel before this call returns so late joiners get
// the container's init segment.
func (t *Track) Subscribe(id string) (<-chan []byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTrackClosed
	}
	if _, exists := t.subscribers[id]; exists {
		return nil, errors.New("room: subscriber already registered")
	}
	ch := make(chan []byte, 64)
	if t.initSegment != nil {
		// Non-blocking by construction: the channel is brand new and has
		// capacity 64, the init segment is one element.
		ch <- t.initSegment
	}
	t.subscribers[id] = ch
	return ch, nil
}

// Unsubscribe removes a subscriber and closes its channel.
func (t *Track) Unsubscribe(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ch, ok := t.subscribers[id]; ok {
		delete(t.subscribers, id)
		close(ch)
	}
}

// Publish broadcasts a chunk to all current subscribers. The chunk is
// copied before being queued so the caller may reuse its buffer. The
// first chunk is additionally retained as the init segment for late
// subscribers.
func (t *Track) Publish(chunk []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTrackClosed
	}
	buf := make([]byte, len(chunk))
	copy(buf, chunk)
	if t.initSegment == nil {
		t.initSegment = buf
	}
	for _, ch := range t.subscribers {
		select {
		case ch <- buf:
		default:
			// subscriber is too slow; drop this chunk for them
		}
	}
	return nil
}

// Close marks the track ended and closes every subscriber channel.
func (t *Track) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	t.initSegment = nil
	for id, ch := range t.subscribers {
		close(ch)
		delete(t.subscribers, id)
	}
}

// Closed reports whether the track has been closed.
func (t *Track) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}
