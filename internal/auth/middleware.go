package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Authenticator verifies incoming requests. Use Required to wrap any
// http.Handler that needs a valid token.
type Authenticator struct {
	Secret []byte
	// Optional: when true, requests without a token still pass through
	// (claims become the zero value). Use only in trusted environments.
	Optional bool
	// Verify is the verification configuration applied to every token.
	Verify VerifyOptions
}

type ctxKey struct{}

// WithClaims attaches the claim set to a context.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext extracts the claims attached by Required. The second return
// value is false when no token was presented (only possible if Optional).
func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}

// Required returns an http.Handler that rejects requests missing or
// carrying an invalid bearer token. The token can be supplied as either:
//
//   - Authorization: Bearer <jwt>             (preferred)
//   - ?token=<jwt> query parameter            (for EventSource / video tags)
func (a *Authenticator) Required(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := extractToken(r)
		if tok == "" {
			if a.Optional {
				next.ServeHTTP(w, r)
				return
			}
			writeAuthError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := VerifyWith(tok, a.Secret, a.Verify)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}

// MintHandler returns an HTTP handler that issues short-lived tokens. It is
// intended for development and integration tests; in production, your own
// auth service should mint tokens against your user database.
//
// POST /api/auth/token
//
//	{ "sub": "user-42", "name": "Alice", "room": "abc123",
//	  "scope": ["track.publish","track.subscribe"], "ttlSeconds": 3600 }
func MintHandler(secret []byte, defaultTTL time.Duration, issuer, audience string) http.Handler {
	type req struct {
		Subject    string   `json:"sub"`
		Name       string   `json:"name"`
		Room       string   `json:"room"`
		Scope      []string `json:"scope"`
		TTLSeconds int      `json:"ttlSeconds"`
	}
	type resp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expiresAt"`
		TokenType string `json:"tokenType"`
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in req
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				writeAuthError(w, http.StatusBadRequest, "invalid json")
				return
			}
		}
		ttl := defaultTTL
		if in.TTLSeconds > 0 {
			ttl = time.Duration(in.TTLSeconds) * time.Second
		}
		now := time.Now()
		c := Claims{
			Subject:  strings.TrimSpace(in.Subject),
			Name:     strings.TrimSpace(in.Name),
			Room:     strings.TrimSpace(in.Room),
			Scope:    in.Scope,
			Issuer:   issuer,
			Audience: audience,
			IssuedAt: now.Unix(),
			Expires:  now.Add(ttl).Unix(),
		}
		token, err := Sign(c, secret)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(resp{
			Token:     token,
			ExpiresAt: c.Expires,
			TokenType: "Bearer",
		})
	})
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(h, "Bearer ") {
			return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	if t := r.URL.Query().Get("access_token"); t != "" {
		return t
	}
	return ""
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("WWW-Authenticate", `Bearer realm="prifa"`)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
