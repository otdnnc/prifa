// Package api exposes the room manager as REST + streaming endpoints.
//
// Routing summary:
//
//	POST   /api/auth/token                                 (dev only) mint token
//	POST   /api/rooms                                      create a room
//	GET    /api/rooms                                      list rooms
//	GET    /api/rooms/{roomID}                             room details
//	POST   /api/rooms/{roomID}/participants                join (returns participant id)
//	DELETE /api/rooms/{roomID}/participants/{pid}          leave
//	GET    /api/rooms/{roomID}/events?participant={pid}    SSE event stream
//	POST   /api/rooms/{roomID}/participants/{pid}/tracks/{kind}   publish media
//	GET    /api/rooms/{roomID}/participants/{pid}/tracks/{kind}   subscribe to media
//	GET    /healthz                                        liveness
//	GET    /readyz                                         readiness
//
// Every /api/* route except /api/auth/token is wrapped with the JWT
// middleware. Static files (when configured) are served unauthenticated
// so the demo page can load before the user has a token.
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"prifa/internal/auth"
	"prifa/internal/logx"
	"prifa/internal/room"
)

// Options configures the public handler.
type Options struct {
	Rooms          *room.Manager
	WebRoot        http.FileSystem
	Auth           *auth.Authenticator
	AllowedOrigins []string // empty list reflects any origin
	DevTokens      http.Handler
}

// Handler is the http.Handler for the public API.
type Handler struct {
	mux            *http.ServeMux
	rooms          *room.Manager
	webRoot        http.FileSystem
	auth           *auth.Authenticator
	allowedOrigins map[string]struct{}
	devTokens      http.Handler
}

// New builds a Handler with explicit options.
func New(opts Options) *Handler {
	h := &Handler{
		mux:       http.NewServeMux(),
		rooms:     opts.Rooms,
		webRoot:   opts.WebRoot,
		auth:      opts.Auth,
		devTokens: opts.DevTokens,
	}
	if len(opts.AllowedOrigins) > 0 {
		h.allowedOrigins = make(map[string]struct{}, len(opts.AllowedOrigins))
		for _, o := range opts.AllowedOrigins {
			h.allowedOrigins[strings.ToLower(o)] = struct{}{}
		}
	}
	h.routes()
	return h
}

// ServeHTTP applies CORS then dispatches to the mux.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) routes() {
	// Authenticated REST + streaming API.
	h.protected("POST /api/rooms", h.createRoom)
	h.protected("GET /api/rooms", h.listRooms)
	h.protected("GET /api/rooms/{roomID}", h.getRoom)
	h.protected("POST /api/rooms/{roomID}/participants", h.joinRoom)
	h.protected("DELETE /api/rooms/{roomID}/participants/{pid}", h.leaveRoom)
	h.protected("GET /api/rooms/{roomID}/events", h.streamEvents)
	h.protected("POST /api/rooms/{roomID}/participants/{pid}/tracks/{kind}", h.publishTrack)
	h.protected("GET /api/rooms/{roomID}/participants/{pid}/tracks/{kind}", h.subscribeTrack)

	// Health probes — unauthenticated by design (load balancers, k8s).
	h.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	h.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ready\n"))
	})

	// Optional dev-only token mint.
	if h.devTokens != nil {
		h.mux.Handle("POST /api/auth/token", h.devTokens)
	}

	if h.webRoot != nil {
		h.mux.Handle("GET /", http.FileServer(h.webRoot))
	}
}

// protected mounts a handler with the auth middleware applied. If no
// authenticator was configured the handler is exposed directly — but
// config.Load already refuses that combination unless -auth-optional is set.
func (h *Handler) protected(pattern string, fn http.HandlerFunc) {
	var handler http.Handler = fn
	if h.auth != nil {
		handler = h.auth.Required(handler)
	}
	h.mux.Handle(pattern, handler)
}

// setCORS allows the demo page (or third-party JS clients) to call the API.
// When AllowedOrigins is configured, only those origins are echoed back —
// any other origin sees no ACAO header and the browser will block the call.
func (h *Handler) setCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		if h.allowedOrigins == nil {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		} else if _, ok := h.allowedOrigins[strings.ToLower(origin)]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
	}
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id")
	w.Header().Set("Access-Control-Expose-Headers", "Alt-Svc, X-Request-Id")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if r != nil && status >= 400 {
		logx.FromContext(r.Context()).Warn("api error",
			"status", status,
			"error", msg,
		)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
