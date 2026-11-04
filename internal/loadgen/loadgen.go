package loadgen

import (
	"context"
	"errors"
	"fmt"
	"leaked_server/internal/protocol"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Mode string

const (
	ModeSilent       Mode = "silent"
	ModeRequest      Mode = "request"
	ModeRequestLeave Mode = "request-leave"
)

type Config struct {
	Addr     string
	Mode     Mode
	Rate     int           // new connections per second
	MaxConns int           // 0 means no cap
	Duration time.Duration // 0 means until ctx is cancelled
	Timeout  time.Duration // dial timeout and I/O deadline in request modes
}

type stats struct {
	open         atomic.Int64
	dialErrs     atomic.Int64
	writeErrs    atomic.Int64
	readErrs     atomic.Int64
	serverClosed atomic.Int64
}

type Client struct {
	cfg   Config
	stats stats
	wg    sync.WaitGroup

	mu      sync.Mutex
	held    map[net.Conn]struct{} // silent connections, kept referenced on purpose
	closing bool
}

func New(cfg Config) (*Client, error) {
	switch cfg.Mode {
	case ModeSilent, ModeRequest, ModeRequestLeave:
	default:
		return nil, fmt.Errorf("invalid mode %q", cfg.Mode)
	}

	if cfg.Rate <= 0 {
		return nil, errors.New("rate must be greater than zero")
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}

	return &Client{
		cfg:  cfg,
		held: make(map[net.Conn]struct{}),
	}, nil
}

func (c *Client) Run(ctx context.Context) error {
	if c.cfg.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.Duration)

		defer cancel()
	}

	c.wg.Go(func() {
		c.report(ctx)
	})

	ticker := time.NewTicker(time.Second / time.Duration(c.cfg.Rate))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.closeHeld()
			c.wg.Wait()

			return nil
		case <-ticker.C:
		}

		if c.cfg.MaxConns > 0 && c.stats.open.Load() >= int64(c.cfg.MaxConns) {
			continue
		}

		c.wg.Go(func() {
			c.openOne(ctx)
		})
	}
}

func (c *Client) openOne(ctx context.Context) {
	d := net.Dialer{
		Timeout: c.cfg.Timeout,
	}

	conn, err := d.DialContext(ctx, "tcp", c.cfg.Addr)
	if err != nil {
		if ctx.Err() == nil {
			c.stats.dialErrs.Add(1)
		}

		return
	}

	c.stats.open.Add(1)

	switch c.cfg.Mode {
	case ModeSilent:
		c.hold(conn)
	case ModeRequest:
		c.request(conn, true)
	case ModeRequestLeave:
		c.request(conn, false)
	}
}

// hold keeps a silent connection open and watches for the server closing it
func (c *Client) hold(conn net.Conn) {
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		conn.Close()
		c.stats.open.Add(-1)

		return
	}

	c.held[conn] = struct{}{}
	c.mu.Unlock()

	c.wg.Go(func() {
		c.watch(conn)
	})
}

// watch blocks on Read. It returns when the server closes the connection
// (fixed server) or when closeHeld closes it (end of the run).
func (c *Client) watch(conn net.Conn) {
	buf := make([]byte, 1)
	conn.Read(buf)

	c.mu.Lock()
	_, stillHeld := c.held[conn]
	if stillHeld {
		delete(c.held, conn)
	}
	c.mu.Unlock()

	// stillHeald means we did not close it ourselves, so the server did.
	if stillHeld {
		conn.Close()
		c.stats.open.Add(-1)
		c.stats.serverClosed.Add(1)
	}
}

func (c *Client) closeHeld() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closing = true

	for conn := range c.held {
		delete(c.held, conn)
		conn.Close()
		c.stats.open.Add(-1)
	}
}

func (c *Client) request(conn net.Conn, waitReply bool) {
	defer c.stats.open.Add(-1)
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(c.cfg.Timeout))

	frame, err := protocol.Message{
		Cmd:     protocol.CmdGet,
		Payload: []byte("byte"),
	}.Marshal()
	if err != nil {
		c.stats.writeErrs.Add(1)

		return
	}

	_, err = conn.Write(frame)
	if err != nil {
		c.stats.writeErrs.Add(1)

		return
	}

	if !waitReply {
		return
	}

	_, err = protocol.NewDecoder(conn).Decode()
	if err != nil {
		c.stats.readErrs.Add(1)
	}
}

func (c *Client) report(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		fmt.Printf(
			"open=%d server_closed=%d dial_errs=%d write_errs=%d read_errs=%d\n",
			c.stats.open.Load(),
			c.stats.serverClosed.Load(),
			c.stats.dialErrs.Load(),
			c.stats.writeErrs.Load(),
			c.stats.readErrs.Load(),
		)
	}
}
