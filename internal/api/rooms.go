package api

import (
	"errors"
	"net/http"
	"strings"

	"prifa/internal/room"
)

type createRoomReq struct {
	Name string `json:"name"`
}

type roomView struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	CreatedAt    any                    `json:"createdAt"`
	Participants []*room.Participant    `json:"participants"`
	Tracks       map[string][]trackView `json:"tracks"`
}

type trackView struct {
	Kind        string `json:"kind"`
	ContentType string `json:"contentType"`
}

func (h *Handler) createRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomReq
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "Untitled room"
	}
	rm := h.rooms.Create(req.Name)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        rm.ID,
		"name":      rm.Name,
		"createdAt": rm.CreatedAt,
	})
}

func (h *Handler) listRooms(w http.ResponseWriter, r *http.Request) {
	rooms := h.rooms.List()
	out := make([]map[string]any, 0, len(rooms))
	for _, rm := range rooms {
		out = append(out, map[string]any{
			"id":               rm.ID,
			"name":             rm.Name,
			"createdAt":        rm.CreatedAt,
			"participantCount": len(rm.Participants()),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": out})
}

func (h *Handler) getRoom(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.lookupRoom(w, r)
	if !ok {
		return
	}
	view := roomView{
		ID:           rm.ID,
		Name:         rm.Name,
		CreatedAt:    rm.CreatedAt,
		Participants: rm.Participants(),
		Tracks:       map[string][]trackView{},
	}
	for _, p := range view.Participants {
		var tracks []trackView
		for _, k := range p.ActiveTracks() {
			if t, ok := p.Track(k); ok {
				tracks = append(tracks, trackView{Kind: string(k), ContentType: t.ContentType()})
			}
		}
		view.Tracks[p.ID] = tracks
	}
	writeJSON(w, http.StatusOK, view)
}

// lookupRoom resolves {roomID} from the path, writing a 404 if unknown.
func (h *Handler) lookupRoom(w http.ResponseWriter, r *http.Request) (*room.Room, bool) {
	id := r.PathValue("roomID")
	rm, err := h.rooms.Get(id)
	if err != nil {
		if errors.Is(err, room.ErrRoomNotFound) {
			writeError(w, http.StatusNotFound, "room not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return nil, false
	}
	return rm, true
}
