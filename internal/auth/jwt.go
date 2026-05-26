// Package auth implements HS256 JWT signing/verification and the HTTP
// middleware that protects the API. The implementation is pure-stdlib so
// the server keeps a minimal dependency footprint.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Claims is the subset of standard JWT claims plus prifa-specific fields.
//
//   - Subject (sub)   external user identity; copied onto Participant.UserID.
//   - Name            display name; overrides the request body's name if set.
//   - Room            when non-empty, the token is bound to that room ID.
//   - Scope           empty means full access; otherwise see HasScope.
//   - Issuer/Audience optional; verified by VerifyOptions when configured.
//   - IssuedAt/Expires/NotBefore are unix seconds.
type Claims struct {
	Subject   string   `json:"sub,omitempty"`
	Name      string   `json:"name,omitempty"`
	Room      string   `json:"room,omitempty"`
	Scope     []string `json:"scope,omitempty"`
	Issuer    string   `json:"iss,omitempty"`
	Audience  string   `json:"aud,omitempty"`
	IssuedAt  int64    `json:"iat,omitempty"`
	NotBefore int64    `json:"nbf,omitempty"`
	Expires   int64    `json:"exp,omitempty"`
}

// Scope tokens recognised by the server. A token with no scopes at all is
// considered fully privileged; scopes are an opt-in restriction.
const (
	ScopeCreateRoom = "room.create"
	ScopeListRooms  = "room.list"
	ScopeJoinRoom   = "room.join"
	ScopePublish    = "track.publish"
	ScopeSubscribe  = "track.subscribe"
)

// AllowsRoom reports whether this token may be used against the given room.
// A token with no Room claim is unbound and may access any room.
func (c Claims) AllowsRoom(roomID string) bool {
	return c.Room == "" || c.Room == roomID
}

// HasScope reports whether the claim carries the requested scope. A token
// with no Scope claim is treated as fully privileged.
func (c Claims) HasScope(s string) bool {
	if len(c.Scope) == 0 {
		return true
	}
	for _, x := range c.Scope {
		if x == s {
			return true
		}
	}
	return false
}

// VerifyOptions narrows what tokens count as valid.
type VerifyOptions struct {
	// Issuer, when set, must match the token's iss claim.
	Issuer string
	// Audience, when set, must match the token's aud claim.
	Audience string
	// ClockSkew is added to NotBefore / Expires checks. Default zero.
	ClockSkew time.Duration
	// Now overrides time.Now (tests).
	Now func() time.Time
}

// Common token errors. They are wrapped with %w so callers can errors.Is.
var (
	ErrInvalidToken = errors.New("auth: invalid token")
	ErrTokenExpired = errors.New("auth: token expired")
	ErrTokenNotYet  = errors.New("auth: token not yet valid")
	ErrBadIssuer    = errors.New("auth: issuer mismatch")
	ErrBadAudience  = errors.New("auth: audience mismatch")
	ErrEmptySecret  = errors.New("auth: empty signing secret")
)

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// Sign produces a compact JWS (HS256) of the given claims.
func Sign(claims Claims, secret []byte) (string, error) {
	if len(secret) == 0 {
		return "", ErrEmptySecret
	}
	if claims.IssuedAt == 0 {
		claims.IssuedAt = time.Now().Unix()
	}
	hb, err := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	head := base64.RawURLEncoding.EncodeToString(hb)
	body := base64.RawURLEncoding.EncodeToString(cb)
	signing := head + "." + body
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signing + "." + sig, nil
}

// Verify parses, signature-checks, and time-checks the token. Use the
// VerifyOptions form to enforce issuer/audience.
func Verify(token string, secret []byte) (Claims, error) {
	return VerifyWith(token, secret, VerifyOptions{})
}

// VerifyWith is Verify with explicit options.
func VerifyWith(token string, secret []byte, opt VerifyOptions) (Claims, error) {
	if len(secret) == 0 {
		return Claims{}, ErrEmptySecret
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: not three segments", ErrInvalidToken)
	}
	head, body, sig := parts[0], parts[1], parts[2]

	hb, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: header b64: %v", ErrInvalidToken, err)
	}
	var h jwtHeader
	if err := json.Unmarshal(hb, &h); err != nil {
		return Claims{}, fmt.Errorf("%w: header json: %v", ErrInvalidToken, err)
	}
	if h.Alg != "HS256" {
		return Claims{}, fmt.Errorf("%w: unsupported alg %q", ErrInvalidToken, h.Alg)
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(head + "." + body))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: signature b64: %v", ErrInvalidToken, err)
	}
	if !hmac.Equal(expected, got) {
		return Claims{}, fmt.Errorf("%w: bad signature", ErrInvalidToken)
	}

	cb, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: payload b64: %v", ErrInvalidToken, err)
	}
	var c Claims
	if err := json.Unmarshal(cb, &c); err != nil {
		return Claims{}, fmt.Errorf("%w: payload json: %v", ErrInvalidToken, err)
	}

	now := time.Now()
	if opt.Now != nil {
		now = opt.Now()
	}
	skew := opt.ClockSkew
	nowSec := now.Unix()
	if c.Expires != 0 && nowSec-int64(skew/time.Second) >= c.Expires {
		return Claims{}, ErrTokenExpired
	}
	if c.NotBefore != 0 && nowSec+int64(skew/time.Second) < c.NotBefore {
		return Claims{}, ErrTokenNotYet
	}
	if opt.Issuer != "" && c.Issuer != opt.Issuer {
		return Claims{}, ErrBadIssuer
	}
	if opt.Audience != "" && c.Audience != opt.Audience {
		return Claims{}, ErrBadAudience
	}
	return c, nil
}
