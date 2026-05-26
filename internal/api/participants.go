package api

import (
	"net/http"
	"strings"

	"prifa/internal/auth"
	"prifa/internal/logx"
)

type joinReq struct {
	Name string `json:"name"`
}

// joinRoom registers a new participant in the room. Identity rules:
//
//   - If a JWT subject is present, it is recorded as Participant.UserID so
//     the calling service can correlate the session back to its own user.
//   - The JWT name claim (if any) overrides the request body's name.
//   - When the token is bound to a specific room, requests against any
//     other room are rejected with 403.
func (h *Handler) joinRoom(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.lookupRoom(w, r)
	if !ok {
		return
	}
	claims, _ := auth.FromContext(r.Context())
	if !claims.AllowsRoom(rm.ID) {
		writeError(w, r, http.StatusForbidden, "token not valid for this room")
		return
	}
	if !claims.HasScope(auth.ScopeJoinRoom) {
		writeError(w, r, http.StatusForbidden, "token missing room.join scope")
		return
	}

	var req joinReq
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid json body")
			return
		}
	}
	name := strings.TrimSpace(req.Name)
	if claims.Name != "" {
		name = claims.Name
	}
	if name == "" {
		name = "guest"
	}
	p := rm.Join(name, claims.Subject)

	logx.FromContext(r.Context()).Info("participant joined",
		"room", rm.ID, "participant", p.ID, "user", claims.Subject, "name", name,
	)
	writeJSON(w, http.StatusCreated, map[string]any{
		"room":        rm.ID,
		"participant": p,
	})
}

// leaveRoom removes the participant. The caller must hold a token for the
// room. We do not require the token's subject to match the participant —
// any operator on the room may evict a peer (e.g. for moderation).
func (h *Handler) leaveRoom(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.lookupRoom(w, r)
	if !ok {
		return
	}
	claims, _ := auth.FromContext(r.Context())
	if !claims.AllowsRoom(rm.ID) {
		writeError(w, r, http.StatusForbidden, "token not valid for this room")
		return
	}
	pid := r.PathValue("pid")
	if err := rm.Leave(pid); err != nil {
		writeError(w, r, http.StatusNotFound, err.Error())
		return
	}
	logx.FromContext(r.Context()).Info("participant left",
		"room", rm.ID, "participant", pid, "actor", claims.Subject,
	)
	w.WriteHeader(http.StatusNoContent)
}
