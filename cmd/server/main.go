package main

import (
	"context"
	"leaked_server/internal/server"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := server.New(":9000")
	if err != nil {
		log.Fatal(err)
	}

	err = srv.Serve(ctx)
	if err != nil {
		log.Fatal(err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = srv.Shutdown(shutdownCtx)
	if err != nil {
		slog.Warn("shutdown", "err", err)
	}
}
