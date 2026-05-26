// Package api exposes the room manager as REST + streaming endpoints.
//
// Routing summary:
//
//	POST   /api/rooms                                      create a room
//	GET    /api/rooms                                      list rooms
//	GET    /api/rooms/{roomID}                             room details
//	POST   /api/rooms/{roomID}/participants                join (returns participant ID)
//	DELETE /api/rooms/{roomID}/participants/{pid}          leave
//	GET    /api/rooms/{roomID}/events?participant={pid}    SSE event stream
//	POST   /api/rooms/{roomID}/participants/{pid}/tracks/{kind}   publish media
//	GET    /api/rooms/{roomID}/participants/{pid}/tracks/{kind}   subscribe to media
package api

import (
	"encoding/json"
	"net/http"

	"prifa/internal/room"
)

// Handler is the http.Handler for the public API.
type Handler struct {
	mux     *http.ServeMux
	rooms   *room.Manager
	webRoot http.FileSystem
}

// New builds a Handler. webRoot may be nil; when set, GET / serves files
// from that filesystem so a static demo client can ship in the same binary.
func New(rooms *room.Manager, webRoot http.FileSystem) *Handler {
	h := &Handler{
		mux:     http.NewServeMux(),
		rooms:   rooms,
		webRoot: webRoot,
	}
	h.routes()
	return h
}

// ServeHTTP applies CORS then dispatches to the mux.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setCORS(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) routes() {
	h.mux.HandleFunc("POST /api/rooms", h.createRoom)
	h.mux.HandleFunc("GET /api/rooms", h.listRooms)
	h.mux.HandleFunc("GET /api/rooms/{roomID}", h.getRoom)
	h.mux.HandleFunc("POST /api/rooms/{roomID}/participants", h.joinRoom)
	h.mux.HandleFunc("DELETE /api/rooms/{roomID}/participants/{pid}", h.leaveRoom)
	h.mux.HandleFunc("GET /api/rooms/{roomID}/events", h.streamEvents)
	h.mux.HandleFunc("POST /api/rooms/{roomID}/participants/{pid}/tracks/{kind}", h.publishTrack)
	h.mux.HandleFunc("GET /api/rooms/{roomID}/participants/{pid}/tracks/{kind}", h.subscribeTrack)

	if h.webRoot != nil {
		h.mux.Handle("GET /", http.FileServer(h.webRoot))
	}
}

// setCORS allows the demo page to call the API from any origin. The server is
// dev-oriented; tighten this list before shipping anywhere public.
func setCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Expose-Headers", "Alt-Svc")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
