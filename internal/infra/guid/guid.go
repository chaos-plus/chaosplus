// Package guid generates globally-unique, roughly time-ordered 64-bit ids using
// Sonyflake. Because an id is produced in-process from time + worker id +
// sequence, it needs no database round-trip and stays unique across cluster
// nodes.
//
// The package spans the whole id-generation feature:
//   - Generator / New — the raw Sonyflake wrapper, seeded with a worker id.
//   - ID — the wire/DB type (string-encoded JSON, huma schema, sql driver).
//   - Module — the application-lifecycle unit that leases a worker id via wuid,
//     installs the process-wide Generator, and serves GET /guid.
package guid

import (
	"errors"
	"sync"
	"time"

	"github.com/sony/sonyflake"
)

// epoch is the Sonyflake start time. Fixed so generated ids stay comparable and
// the 39-bit time field lasts ~174 years from here.
var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ErrNotInitialized is returned by the package-level helpers before SetDefault.
var ErrNotInitialized = errors.New("guid: default generator not initialized")

// Generator produces ids from a single worker id.
type Generator struct {
	sf *sonyflake.Sonyflake
}

// New builds a Generator for the given worker id (0..65535).
func New(workerID uint16) (*Generator, error) {
	sf, err := sonyflake.New(sonyflake.Settings{
		StartTime: epoch,
		MachineID: func() (uint16, error) { return workerID, nil },
	})
	if err != nil {
		return nil, err
	}
	return &Generator{sf: sf}, nil
}

// Next returns the next id. It only errors in the astronomically rare case that
// Sonyflake's time bits overflow (~174 years after the epoch).
func (g *Generator) Next() (int64, error) {
	id, err := g.sf.NextID()
	if err != nil {
		return 0, err
	}
	return int64(id), nil
}

// MustNext is Next without error handling; it panics on the rare overflow error.
func (g *Generator) MustNext() int64 {
	id, err := g.Next()
	if err != nil {
		panic(err)
	}
	return id
}

// ---- process-wide default, convenient for model hooks ----

var (
	mu  sync.RWMutex
	def *Generator
)

// SetDefault installs the process-wide default generator. Call once at startup
// after the worker id is known, e.g. guid.SetDefault(g).
func SetDefault(g *Generator) {
	mu.Lock()
	def = g
	mu.Unlock()
}

// Default returns the current default generator (nil if unset).
func Default() *Generator {
	mu.RLock()
	defer mu.RUnlock()
	return def
}

// Next returns an id from the default generator.
func Next() (int64, error) {
	g := Default()
	if g == nil {
		return 0, ErrNotInitialized
	}
	return g.Next()
}

// MustNext returns an id from the default generator; it panics if unset or on
// the rare overflow error.
func MustNext() int64 {
	id, err := Next()
	if err != nil {
		panic(err)
	}
	return id
}
