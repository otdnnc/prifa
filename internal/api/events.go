package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// streamEvents pushes room events to the client as Server-Sent Events.
// The transport sits on top of whatever HTTP version negotiated below;
// over HTTP/3 the same body is delivered through a QUIC stream.
func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.lookupRoom(w, r)
	if !ok {
		return
	}
	pid := r.URL.Query().Get("participant")
	if pid == "" {
		writeError(w, http.StatusBadRequest, "participant query param required")
		return
	}
	if _, ok := rm.Participant(pid); !ok {
		writeError(w, http.StatusNotFound, "participant not in room")
		return
	}

	events, err := rm.SubscribeEvents(pid)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	defer rm.UnsubscribeEvents(pid)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	// initial comment forces the client to consider headers received
	fmt.Fprintf(w, ": connected to %s as %s\n\n", rm.ID, pid)
	flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
			flush()
		}
	}
}
