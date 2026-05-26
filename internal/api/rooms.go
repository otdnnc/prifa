package api

import (
	"errors"
	"net/http"
	"strings"

	"prifa/internal/auth"
	"prifa/internal/logx"
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
	claims, _ := auth.FromContext(r.Context())
	if !claims.HasScope(auth.ScopeCreateRoom) {
		writeError(w, r, http.StatusForbidden, "token missing room.create scope")
		return
	}
	// A token bound to a specific room id cannot create new rooms.
	if claims.Room != "" {
		writeError(w, r, http.StatusForbidden, "token is bound to room "+claims.Room)
		return
	}

	var req createRoomReq
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid json body")
			return
		}
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "Untitled room"
	}
	rm := h.rooms.Create(req.Name)
	logx.FromContext(r.Context()).Info("room created",
		"room", rm.ID, "name", rm.Name, "actor", claims.Subject,
	)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        rm.ID,
		"name":      rm.Name,
		"createdAt": rm.CreatedAt,
	})
}

func (h *Handler) listRooms(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	if !claims.HasScope(auth.ScopeListRooms) {
		writeError(w, r, http.StatusForbidden, "token missing room.list scope")
		return
	}
	rooms := h.rooms.List()
	out := make([]map[string]any, 0, len(rooms))
	for _, rm := range rooms {
		// If the token is room-bound, only surface that single room.
		if claims.Room != "" && claims.Room != rm.ID {
			continue
		}
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
	claims, _ := auth.FromContext(r.Context())
	if !claims.AllowsRoom(rm.ID) {
		writeError(w, r, http.StatusForbidden, "token not valid for this room")
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
			writeError(w, r, http.StatusNotFound, "room not found")
		} else {
			writeError(w, r, http.StatusInternalServerError, err.Error())
		}
		return nil, false
	}
	return rm, true
}
