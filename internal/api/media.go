package api

import (
	"errors"
	"io"
	"net/http"

	"prifa/internal/room"
)

const (
	// publishReadSize is the maximum chunk we read off the publisher's body
	// in a single Read. Browsers' MediaRecorder typically emits 1-100 KB
	// fragments, so this is a generous ceiling.
	publishReadSize = 64 * 1024
)

// publishTrack ingests a streaming POST and fans it out through the room's
// Track abstraction. The request body stays open for the life of the call;
// when the publisher's stream ends or the connection drops, the track is
// closed and subscribers are notified.
func (h *Handler) publishTrack(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.lookupRoom(w, r)
	if !ok {
		return
	}
	pid := r.PathValue("pid")
	p, ok := rm.Participant(pid)
	if !ok {
		writeError(w, http.StatusNotFound, "participant not in room")
		return
	}
	kind := room.TrackKind(r.PathValue("kind"))
	if !kind.Valid() {
		writeError(w, http.StatusBadRequest, "unknown track kind")
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	track, err := p.StartTrack(kind, contentType)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	rm.AnnounceTrackStarted(pid, kind, contentType)
	defer func() {
		p.EndTrack(kind)
		rm.AnnounceTrackEnded(pid, kind)
	}()

	// Acknowledge the upload so the client can confirm the track exists.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		_, _ = io.WriteString(w, `{"status":"streaming"}`+"\n")
		flusher.Flush()
	}

	buf := make([]byte, publishReadSize)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			if perr := track.Publish(buf[:n]); perr != nil {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return
			}
			// Client disconnect or stream reset: stop quietly.
			return
		}
	}
}

// subscribeTrack streams a live track to the subscriber's response body.
// The handler holds the request open until the publisher ends or the
// subscriber disconnects.
func (h *Handler) subscribeTrack(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.lookupRoom(w, r)
	if !ok {
		return
	}
	pid := r.PathValue("pid")
	p, ok := rm.Participant(pid)
	if !ok {
		writeError(w, http.StatusNotFound, "participant not in room")
		return
	}
	kind := room.TrackKind(r.PathValue("kind"))
	if !kind.Valid() {
		writeError(w, http.StatusBadRequest, "unknown track kind")
		return
	}
	track, ok := p.Track(kind)
	if !ok {
		writeError(w, http.StatusNotFound, "track not active")
		return
	}

	subscriberID := r.URL.Query().Get("subscriber")
	if subscriberID == "" {
		subscriberID = randomSubID()
	}
	chunks, err := track.Subscribe(subscriberID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	defer track.Unsubscribe(subscriberID)

	w.Header().Set("Content-Type", track.ContentType())
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-chunks:
			if !ok {
				return
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func randomSubID() string {
	// 4 random bytes is plenty for a unique subscriber tag inside one track.
	return "sub-" + room.NewID(4)
}
