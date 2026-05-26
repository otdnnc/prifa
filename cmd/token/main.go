// Command token mints HS256 JWTs that the prifa server will accept.
//
//	go run ./cmd/token -sub user-42 -name Alice -room abc123 -ttl 1h \
//	    -scope track.publish,track.subscribe -secret "$PRIFA_JWT_SECRET"
//
// The same secret must be configured on the server. Pass -json to emit the
// claims alongside the encoded token for debugging.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"prifa/internal/auth"
)

func main() {
	secretFlag := flag.String("secret", os.Getenv("PRIFA_JWT_SECRET"), "HS256 signing secret (defaults to $PRIFA_JWT_SECRET)")
	sub := flag.String("sub", "", "subject (your external user id) — written to Participant.UserID")
	name := flag.String("name", "", "display name (overrides the join request's name)")
	room := flag.String("room", "", "room id; empty makes the token valid for any room")
	scope := flag.String("scope", "", "comma-separated scopes; empty grants full access")
	issuer := flag.String("iss", "", "issuer (iss) claim — must match server's -jwt-issuer if set")
	audience := flag.String("aud", "", "audience (aud) claim — must match server's -jwt-audience if set")
	ttl := flag.Duration("ttl", time.Hour, "token lifetime; 0 means no expiry (NOT recommended)")
	asJSON := flag.Bool("json", false, "also print decoded claims as JSON")
	flag.Parse()

	if *secretFlag == "" {
		log.Fatal("token: -secret (or $PRIFA_JWT_SECRET) is required")
	}

	now := time.Now()
	claims := auth.Claims{
		Subject:  strings.TrimSpace(*sub),
		Name:     strings.TrimSpace(*name),
		Room:     strings.TrimSpace(*room),
		Scope:    splitCSV(*scope),
		Issuer:   strings.TrimSpace(*issuer),
		Audience: strings.TrimSpace(*audience),
		IssuedAt: now.Unix(),
	}
	if *ttl > 0 {
		claims.Expires = now.Add(*ttl).Unix()
	}

	token, err := auth.Sign(claims, []byte(*secretFlag))
	if err != nil {
		log.Fatalf("token: sign: %v", err)
	}

	if *asJSON {
		out := map[string]any{"token": token, "claims": claims}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	fmt.Println(token)
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
