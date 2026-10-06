package kubeconfig

import "sync"

// Store holds the effective configuration and the name of the current
// context. It is safe for concurrent use: readers always observe one
// fully-consistent snapshot, never a mix of old and new state.
type Store struct {
	mu      sync.RWMutex
	config  *Config
	current string
	path    string // file switches are persisted to
}

// NewStore creates a Store backed by the given config. PersistPath may
// be empty, in which case Switch only updates memory.
func NewStore(cfg *Config, persistPath string) *Store {
	return nil
}

// Current returns the name of the active context.
func (s *Store) Current() string { return "" }

// Snapshot returns the configuration as of the last successful switch.
func (s *Store) Snapshot() *Config { return nil }

// Switch atomically moves to another context: concurrent readers see
// either the full old state or the full new state, and the on-disk
// file is replaced atomically (temp file + fsync + rename).
func (s *Store) Switch(name string) error { return nil }
