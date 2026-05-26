package auth

import (
	"errors"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("a-test-secret-at-least-32-bytes!!")
	in := Claims{
		Subject:  "user-7",
		Name:     "Alice",
		Room:     "room-1",
		Scope:    []string{ScopePublish, ScopeSubscribe},
		Issuer:   "test",
		Audience: "prifa",
		Expires:  time.Now().Add(time.Hour).Unix(),
	}
	tok, err := Sign(in, secret)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	out, err := VerifyWith(tok, secret, VerifyOptions{Issuer: "test", Audience: "prifa"})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.Subject != in.Subject || out.Room != in.Room {
		t.Fatalf("claims mismatch: %+v", out)
	}
	if !out.AllowsRoom("room-1") || out.AllowsRoom("room-2") {
		t.Fatal("room binding not enforced")
	}
	if !out.HasScope(ScopePublish) || out.HasScope("other") {
		t.Fatal("scope check broken")
	}
}

func TestVerifyRejectsTampered(t *testing.T) {
	secret := []byte("a-test-secret-at-least-32-bytes!!")
	tok, err := Sign(Claims{Subject: "u"}, secret)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	bad := tok[:len(tok)-2] + "AA"
	if _, err := Verify(bad, secret); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestVerifyExpired(t *testing.T) {
	secret := []byte("a-test-secret-at-least-32-bytes!!")
	tok, _ := Sign(Claims{Subject: "u", Expires: time.Now().Add(-time.Second).Unix()}, secret)
	if _, err := Verify(tok, secret); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestEmptyScopeMeansFull(t *testing.T) {
	c := Claims{}
	if !c.HasScope(ScopePublish) || !c.HasScope("anything") {
		t.Fatal("empty scope should allow everything")
	}
}

func TestRoomBinding(t *testing.T) {
	c := Claims{Room: "a"}
	if !c.AllowsRoom("a") {
		t.Fatal("should allow bound room")
	}
	if c.AllowsRoom("b") {
		t.Fatal("should reject other room")
	}
	if !(Claims{}).AllowsRoom("anything") {
		t.Fatal("unbound token should allow any room")
	}
}
