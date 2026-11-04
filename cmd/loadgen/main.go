package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"leaked_server/internal/loadgen"
)

func main() {
	addr := flag.String("addr", "server:9000", "target host:port")
	mode := flag.String("mode", "silent", "silent, request or request-leave")
	rate := flag.Int("rate", 50, "new connections per second")
	maxConns := flag.Int("max-conns", 0, "cap on open connections, 0 means no cap")
	duration := flag.Duration("duration", 0, "total run time, 0 means until stopped")
	timeout := flag.Duration("timeout", 5*time.Second, "dial timeout and I/O deadline")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := loadgen.New(loadgen.Config{
		Addr:     *addr,
		Mode:     loadgen.Mode(*mode),
		Rate:     *rate,
		MaxConns: *maxConns,
		Duration: *duration,
		Timeout:  *timeout,
	})
	if err != nil {
		log.Fatal(err)
	}

	err = c.Run(ctx)
	if err != nil {
		log.Fatal(err)
	}
}
