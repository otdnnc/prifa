package api

import (
	"net/http"
	"strings"
)

type joinReq struct {
	Name string `json:"name"`
}

func (h *Handler) joinRoom(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.lookupRoom(w, r)
	if !ok {
		return
	}
	var req joinReq
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "guest"
	}
	p := rm.Join(req.Name)
	writeJSON(w, http.StatusCreated, map[string]any{
		"room":        rm.ID,
		"participant": p,
	})
}

func (h *Handler) leaveRoom(w http.ResponseWriter, r *http.Request) {
	rm, ok := h.lookupRoom(w, r)
	if !ok {
		return
	}
	pid := r.PathValue("pid")
	if err := rm.Leave(pid); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
