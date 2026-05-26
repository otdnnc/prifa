// Command prifa is a production-ready, JWT-authenticated HTTP/3 video-call
// server backed by in-memory rooms.
//
//	prifa -addr :8443 \
//	      -cert certs/cert.pem -key certs/key.pem \
//	      -jwt-secret "$(openssl rand -hex 32)"
//
// See README.md for deployment, JWT minting, and JavaScript integration.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"prifa/internal/api"
	"prifa/internal/auth"
	"prifa/internal/config"
	"prifa/internal/logx"
	"prifa/internal/room"
	"prifa/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "prifa: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}

	logger := logx.New(os.Stdout, logx.ParseLevel(cfg.LogLevel), logx.Format(cfg.LogFormat))
	slog.SetDefault(logger)
	logger.Info("starting prifa", "config", cfg.Redacted())

	rooms := room.NewManager()
	go janitor(rooms, logger)

	var webFS http.FileSystem
	if cfg.WebDir != "" {
		if info, err := os.Stat(cfg.WebDir); err == nil && info.IsDir() {
			webFS = http.Dir(cfg.WebDir)
			logger.Info("serving static client", "dir", cfg.WebDir)
		} else {
			logger.Warn("web dir not found, demo client disabled", "dir", cfg.WebDir)
		}
	}

	var authn *auth.Authenticator
	if len(cfg.JWTSecret) > 0 {
		authn = &auth.Authenticator{
			Secret:   cfg.JWTSecret,
			Optional: cfg.AuthOptional,
			Verify: auth.VerifyOptions{
				Issuer:   cfg.JWTIssuer,
				Audience: cfg.JWTAudience,
			},
		}
	} else if cfg.AuthOptional {
		// auth-optional with no secret: any request is allowed through.
		authn = &auth.Authenticator{Optional: true}
	}

	var devTokens http.Handler
	if cfg.EnableDevTokens {
		devTokens = auth.MintHandler(cfg.JWTSecret, cfg.DevTokenTTL, cfg.JWTIssuer, cfg.JWTAudience)
		logger.Warn("POST /api/auth/token is enabled — do not run this in production")
	}

	apiHandler := api.New(api.Options{
		Rooms:          rooms,
		WebRoot:        webFS,
		Auth:           authn,
		AllowedOrigins: cfg.AllowedOrigins,
		DevTokens:      devTokens,
	})

	handler := logx.AccessLog(logger)(apiHandler)

	srv, err := server.New(server.Config{
		Addr:        cfg.Addr,
		CertFile:    cfg.CertFile,
		KeyFile:     cfg.KeyFile,
		Handler:     handler,
		EnableHTTPS: !cfg.DisableHTTPS,
		Logger:      logger,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("server stopped: %w", err)
	}
	logger.Info("bye")
	return nil
}

// janitor periodically sweeps rooms that have no participants left. This
// keeps long-running deployments from accumulating dead room entries.
func janitor(rooms *room.Manager, logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if n := rooms.SweepEmpty(); n > 0 {
			logger.Info("janitor swept empty rooms", "count", n)
		}
	}
}
