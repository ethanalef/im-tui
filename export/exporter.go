package export

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Config controls the JSONL export behavior.
type Config struct {
	Enabled  bool
	Path     string
	Interval time.Duration
}

// Exporter writes JSONL records to a file, throttled to a configured interval.
type Exporter struct {
	mu        sync.Mutex
	file      *os.File
	interval  time.Duration
	lastWrite time.Time
	seq       int
	agg       *aggregator
	closed    bool
}

// New creates an Exporter, truncates the file, and writes the session_start record.
func New(cfg Config, start SessionStartRecord) (*Exporter, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("export: open %s: %w", cfg.Path, err)
	}

	start.Type = TypeSessionStart
	start.Timestamp = time.Now()

	if err := writeLine(f, start); err != nil {
		f.Close()
		return nil, fmt.Errorf("export: write session_start: %w", err)
	}

	interval := cfg.Interval
	if interval == 0 {
		interval = 10 * time.Second
	}

	return &Exporter{
		file:     f,
		interval: interval,
		agg:      newAggregator(),
	}, nil
}

// OnUpdate is called from the TUI update loop. It throttles writes to the
// configured interval and always aggregates for the session summary.
func (e *Exporter) OnUpdate(snap SnapshotRecord) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return
	}

	// Always aggregate regardless of write throttle
	e.agg.ingest(&snap)
	if len(snap.Alerts.Active) > 0 {
		e.agg.ingestAlerts(snap.Alerts.Active)
	}

	// Throttle file writes
	now := time.Now()
	if now.Sub(e.lastWrite) < e.interval {
		return
	}

	e.seq++
	snap.Type = TypeSnapshot
	snap.Timestamp = now
	snap.Seq = e.seq

	if err := writeLine(e.file, snap); err != nil {
		// Best effort — don't crash the TUI for export errors
		return
	}
	e.lastWrite = now
}

// Close writes the session_end record and closes the file.
func (e *Exporter) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	now := time.Now()
	dur := now.Sub(e.agg.startTime)

	endRecord := SessionEndRecord{
		Type:          TypeSessionEnd,
		Timestamp:     now,
		Duration:      dur.Round(time.Second).String(),
		DurationSec:   dur.Seconds(),
		SnapshotCount: e.agg.snapshotCount,
		Summary:       e.agg.summary(),
	}

	if err := writeLine(e.file, endRecord); err != nil {
		e.file.Close()
		return fmt.Errorf("export: write session_end: %w", err)
	}

	return e.file.Close()
}

// writeLine JSON-encodes v and writes it as a single line with a trailing newline.
func writeLine(f *os.File, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}
