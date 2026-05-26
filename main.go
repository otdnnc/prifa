// Command prifa is an HTTP/3 video-call server backed by in-memory rooms.
//
//	go run . -addr :8443 -cert certs/cert.pem -key certs/key.pem
//
// See README.md for a full walkthrough.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"prifa/internal/api"
	"prifa/internal/room"
	"prifa/internal/server"
)

func main() {
	addr := flag.String("addr", ":8443", "address to listen on (TCP and UDP)")
	certFile := flag.String("cert", "certs/cert.pem", "path to TLS certificate")
	keyFile := flag.String("key", "certs/key.pem", "path to TLS private key")
	webDir := flag.String("web", "web", "directory of static demo client (empty to disable)")
	noHTTPS := flag.Bool("no-https", false, "disable the parallel HTTPS listener (HTTP/3 only)")
	flag.Parse()

	rooms := room.NewManager()

	var webFS http.FileSystem
	if *webDir != "" {
		if info, err := os.Stat(*webDir); err == nil && info.IsDir() {
			webFS = http.Dir(*webDir)
			log.Printf("main: serving static client from %s", *webDir)
		} else {
			log.Printf("main: web dir %s not found, demo client disabled", *webDir)
		}
	}

	handler := api.New(rooms, webFS)

	srv, err := server.New(server.Config{
		Addr:        *addr,
		CertFile:    *certFile,
		KeyFile:     *keyFile,
		Handler:     handler,
		EnableHTTPS: !*noHTTPS,
	})
	if err != nil {
		log.Fatalf("main: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("main: server stopped: %v", err)
	}
	log.Printf("main: bye")
}
