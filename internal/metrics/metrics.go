package metrics

import (
	"encoding/csv"
	"fmt"
	"os"
	"time"
)

var header = []string{
	"ts_unix_ms",
	"goroutines",
	"heap_alloc_bytes",
	"heap_sys_bytes",
	"stack_inuse_bytes",
	"rss_bytes",
	"cgroup_mem_bytes",
	"open_conns",
}

type Collector struct {
	interval  time.Duration
	openConns func() int64

	f *os.File
	w *csv.Writer
}

func New(path string, interval time.Duration, openConns func() int64) (*Collector, error) {
	// os.Create would erase the file content on a fresh start, needs to just open instead
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open metrics file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()

		return nil, fmt.Errorf("stat metrics file: %w", err)
	}

	w := csv.NewWriter(f)
	err = w.Write(header)
	if err != nil && info.Size() == 0 {
		f.Close()

		return nil, fmt.Errorf("write header: %w", err)
	}
	w.Flush()

	err = w.Error()
	if err != nil {
		f.Close()

		return nil, fmt.Errorf("flush header: %w", err)
	}

	return &Collector{
		interval:  interval,
		openConns: openConns,
		f:         f,
		w:         w,
	}, nil
}
