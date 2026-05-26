// Package server wires the API handler onto an HTTP/3 (QUIC) listener
// and a parallel HTTPS (TCP) listener that advertises the h3 endpoint.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// Config controls the listener setup.
type Config struct {
	// Addr is the host:port to listen on. The same port is used for TCP/443
	// (HTTPS) and UDP/443 (HTTP/3).
	Addr string

	// CertFile/KeyFile point at a PEM-encoded certificate and private key.
	CertFile string
	KeyFile  string

	// Handler is the application handler.
	Handler http.Handler

	// EnableHTTPS toggles the TCP HTTPS listener. HTTP/3-only deployments
	// can disable it, though most browsers need it to discover Alt-Svc.
	EnableHTTPS bool
}

// Server runs both listeners and coordinates shutdown.
type Server struct {
	cfg   Config
	h3    *http3.Server
	https *http.Server
}

// New builds a Server from the given config. It does not start listeners.
func New(cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		return nil, errors.New("server: Addr is required")
	}
	if cfg.Handler == nil {
		return nil, errors.New("server: Handler is required")
	}
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, errors.New("server: CertFile and KeyFile are required")
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("server: load tls cert: %w", err)
	}

	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3", "h2", "http/1.1"},
		MinVersion:   tls.VersionTLS13,
	}

	h3srv := &http3.Server{
		Addr:      cfg.Addr,
		TLSConfig: http3.ConfigureTLSConfig(tlsConf.Clone()),
		Handler:   cfg.Handler,
		QUICConfig: &quic.Config{
			MaxIdleTimeout:  60 * time.Second,
			KeepAlivePeriod: 20 * time.Second,
		},
	}

	s := &Server{cfg: cfg, h3: h3srv}

	if cfg.EnableHTTPS {
		// Wrap the handler so every HTTPS response carries Alt-Svc, telling
		// the browser that the same origin is reachable over HTTP/3.
		wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := h3srv.SetQUICHeaders(w.Header()); err != nil {
				log.Printf("server: set Alt-Svc: %v", err)
			}
			cfg.Handler.ServeHTTP(w, r)
		})
		s.https = &http.Server{
			Addr:              cfg.Addr,
			Handler:           wrapped,
			TLSConfig:         tlsConf,
			ReadHeaderTimeout: 10 * time.Second,
		}
	}

	return s, nil
}

// Run starts both listeners and blocks until ctx is cancelled or a
// listener returns an unrecoverable error.
func (s *Server) Run(ctx context.Context) error {
	errs := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("server: HTTP/3 (QUIC) listening on udp %s", s.cfg.Addr)
		if err := s.h3.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			errs <- fmt.Errorf("h3: %w", err)
			return
		}
		errs <- nil
	}()

	if s.https != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("server: HTTPS (TCP) listening on tcp %s", s.cfg.Addr)
			if err := s.https.ListenAndServeTLS(s.cfg.CertFile, s.cfg.KeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- fmt.Errorf("https: %w", err)
				return
			}
			errs <- nil
		}()
	}

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errs:
		if err != nil {
			_ = s.shutdown()
			return err
		}
		// One listener exited cleanly; bring the other one down too.
		return s.shutdown()
	}
}

func (s *Server) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var firstErr error
	if s.https != nil {
		if err := s.https.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}
	if err := s.h3.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
