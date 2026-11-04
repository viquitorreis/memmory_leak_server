package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"leaked_server/internal/protocol"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Server struct {
	ln        net.Listener
	wg        sync.WaitGroup
	openConns atomic.Int64
}

// New binds the listener right away, so every other method can trust s.ln.
func New(addr string) (*Server, error) {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}

	return &Server{
		ln: ln,
	}, nil
}

// Addr returns the real address, needed when listening on ":0" in tests.
func (s *Server) Addr() net.Addr {
	return s.ln.Addr()
}

func (s *Server) OpenConns() int64 {
	return s.openConns.Load()
}

// Serve accepts connections until ctx is cancelled or the listener is closed.
func (s *Server) Serve(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		s.ln.Close()
	})
	defer stop()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}

			// Errors like "too many open files" are not fatal: keep the server alive.
			slog.Warn("accept failed", "err", err)
			time.Sleep(10 * time.Millisecond)

			continue
		}

		s.openConns.Add(1)

		s.wg.Go(func() {
			s.handleClient(conn)
		})
	}
}

// Shutdown stops accepting and waits for handlers until ctx expires.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.ln.Close()
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close listener: %w", err)
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%d handlers still running: %w", s.openConns.Load(), ctx.Err())
	}
}

func (s *Server) handleClient(conn net.Conn) {
	defer s.openConns.Add(-1)
	defer conn.Close()

	dec := protocol.NewDecoder(conn)

	for {
		msg, err := dec.Decode()
		if err != nil {
			// io.EOF is the client closing cleanly between messages, not an error.
			if !errors.Is(err, io.EOF) {
				slog.Warn("read failed", "remote", conn.RemoteAddr().String(), "err", err)
			}

			return
		}

		slog.Debug("message received", "cmd", msg.Cmd, "size", len(msg.Payload))
	}
}
