// Package config loads runtime configuration from CLI flags and the
// environment. Environment variables win over flag defaults so 12-factor
// container deployments can stay clean.
package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved configuration for one server process.
type Config struct {
	// Listen settings.
	Addr        string
	CertFile    string
	KeyFile     string
	WebDir      string
	DisableHTTPS bool

	// Auth settings. JWTSecret must be set unless AuthOptional is true.
	JWTSecret        []byte
	JWTIssuer        string
	JWTAudience      string
	AuthOptional     bool          // when true, missing tokens are accepted
	EnableDevTokens  bool          // mount POST /api/auth/token
	DevTokenTTL      time.Duration // TTL for tokens minted by /api/auth/token

	// CORS allowlist. Empty list means reflect any origin (legacy demo
	// behaviour). Use this in production to lock the API to known callers.
	AllowedOrigins []string

	// Logging.
	LogLevel  string
	LogFormat string
}

// Load parses flags from args (without the program name) and the
// environment, applies defaults, and returns the result. It writes usage
// to os.Stderr if -h/-help is passed.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("prifa", flag.ContinueOnError)

	addr := fs.String("addr", envOr("PRIFA_ADDR", ":8443"), "address to listen on (TCP + UDP). env PRIFA_ADDR")
	certFile := fs.String("cert", envOr("PRIFA_CERT", "certs/cert.pem"), "TLS certificate path. env PRIFA_CERT")
	keyFile := fs.String("key", envOr("PRIFA_KEY", "certs/key.pem"), "TLS private key path. env PRIFA_KEY")
	webDir := fs.String("web", envOr("PRIFA_WEB_DIR", "web"), "static client directory; empty to disable. env PRIFA_WEB_DIR")
	noHTTPS := fs.Bool("no-https", envBool("PRIFA_NO_HTTPS", false), "disable the TCP HTTPS listener (HTTP/3 only). env PRIFA_NO_HTTPS")

	jwtSecret := fs.String("jwt-secret", envOr("PRIFA_JWT_SECRET", ""), "HS256 JWT signing secret. PREFER env PRIFA_JWT_SECRET so the secret never lands in your shell history")
	jwtIssuer := fs.String("jwt-issuer", envOr("PRIFA_JWT_ISSUER", ""), "expected JWT iss claim; empty disables the check. env PRIFA_JWT_ISSUER")
	jwtAudience := fs.String("jwt-audience", envOr("PRIFA_JWT_AUDIENCE", ""), "expected JWT aud claim; empty disables the check. env PRIFA_JWT_AUDIENCE")
	authOptional := fs.Bool("auth-optional", envBool("PRIFA_AUTH_OPTIONAL", false), "accept requests without a token (DEV ONLY). env PRIFA_AUTH_OPTIONAL")
	devTokens := fs.Bool("dev-tokens", envBool("PRIFA_DEV_TOKENS", false), "expose POST /api/auth/token to mint short-lived tokens (DEV ONLY). env PRIFA_DEV_TOKENS")
	devTokenTTL := fs.Duration("dev-token-ttl", envDuration("PRIFA_DEV_TOKEN_TTL", time.Hour), "default TTL for dev-minted tokens. env PRIFA_DEV_TOKEN_TTL")

	allowed := fs.String("allowed-origins", envOr("PRIFA_ALLOWED_ORIGINS", ""), "comma-separated CORS allowlist; empty reflects any origin. env PRIFA_ALLOWED_ORIGINS")

	logLevel := fs.String("log-level", envOr("PRIFA_LOG_LEVEL", "info"), "debug|info|warn|error. env PRIFA_LOG_LEVEL")
	logFormat := fs.String("log-format", envOr("PRIFA_LOG_FORMAT", "text"), "text|json. env PRIFA_LOG_FORMAT")

	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:            *addr,
		CertFile:        *certFile,
		KeyFile:         *keyFile,
		WebDir:          *webDir,
		DisableHTTPS:    *noHTTPS,
		JWTSecret:       []byte(*jwtSecret),
		JWTIssuer:       *jwtIssuer,
		JWTAudience:     *jwtAudience,
		AuthOptional:    *authOptional,
		EnableDevTokens: *devTokens,
		DevTokenTTL:     *devTokenTTL,
		AllowedOrigins:  splitCSV(*allowed),
		LogLevel:        *logLevel,
		LogFormat:       *logFormat,
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.Addr == "" {
		return errors.New("config: addr required")
	}
	if c.CertFile == "" || c.KeyFile == "" {
		return errors.New("config: cert and key are required")
	}
	if len(c.JWTSecret) == 0 && !c.AuthOptional {
		return errors.New("config: jwt-secret is required (or pass -auth-optional for development)")
	}
	if len(c.JWTSecret) > 0 && len(c.JWTSecret) < 16 {
		return fmt.Errorf("config: jwt-secret looks too short (%d bytes); use at least 32 random bytes", len(c.JWTSecret))
	}
	if c.EnableDevTokens && len(c.JWTSecret) == 0 {
		return errors.New("config: -dev-tokens requires a JWT secret")
	}
	return nil
}

// Redacted returns a copy of the config with the JWT secret removed so it
// can be safely emitted to logs at startup.
func (c Config) Redacted() map[string]any {
	return map[string]any{
		"addr":             c.Addr,
		"cert":             c.CertFile,
		"key":              c.KeyFile,
		"web_dir":          c.WebDir,
		"https_disabled":   c.DisableHTTPS,
		"jwt_configured":   len(c.JWTSecret) > 0,
		"jwt_issuer":       c.JWTIssuer,
		"jwt_audience":     c.JWTAudience,
		"auth_optional":    c.AuthOptional,
		"dev_tokens":       c.EnableDevTokens,
		"dev_token_ttl":    c.DevTokenTTL.String(),
		"allowed_origins":  c.AllowedOrigins,
		"log_level":        c.LogLevel,
		"log_format":       c.LogFormat,
	}
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off", "":
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	return def
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
