// Command server serves the game and its API.
//
//	JWT_SECRET=$(openssl rand -hex 32) go run ./cmd/server -addr :8080
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"spaceinvaders/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	web := flag.String("web", "web", "directory with index.html and main.wasm")
	data := flag.String("data", "data/store.json", "file for accounts and scores; empty keeps them in memory")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log, *addr, *web, *data); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, addr, web, data string) error {
	secret := []byte(os.Getenv("JWT_SECRET"))
	if len(secret) == 0 {
		// Fine for a classroom: sessions end when the server restarts.
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		log.Warn("JWT_SECRET is not set: using a random key, sessions will not survive a restart")
	}
	if _, err := os.Stat(filepath.Join(web, "main.wasm")); err != nil {
		return errors.New(filepath.Join(web, "main.wasm") + " not found: run ./build.sh first")
	}
	if data != "" {
		if err := os.MkdirAll(filepath.Dir(data), 0o700); err != nil {
			return err
		}
	}
	store, err := server.OpenStore(data)
	if err != nil {
		return err
	}
	app, err := server.New(server.Config{Secret: secret, Store: store, WebDir: web, Logger: log})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           app,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	log.Info("listening", "addr", addr, "web", web, "data", data)

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
