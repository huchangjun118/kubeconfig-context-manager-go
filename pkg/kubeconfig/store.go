package kubeconfig

import (
	"sync"
	"sync/atomic"
)

// snapshot is the unit readers observe: one config plus the current
// context name, always consistent with each other.
type snapshot struct {
	config  *Config
	current string
}

// Store holds the effective configuration and the name of the current
// context. Readers load an immutable snapshot atomically, so at any
// instant they see one fully-consistent state — never a mix of an old
// context name with a new config or vice versa.
type Store struct {
	snap atomic.Pointer[snapshot]

	// mu serializes Switch (read-modify-write-persist). Readers never
	// take it, so reads stay cheap and wait-free.
	mu   sync.Mutex
	path string // file switches are persisted to; "" disables persistence
}

// NewStore creates a Store backed by the given config. persistPath may
// be empty, in which case Switch only updates memory.
func NewStore(cfg *Config, persistPath string) *Store {
	s := &Store{path: persistPath}
	s.snap.Store(&snapshot{config: cfg, current: cfg.CurrentContext})
	return s
}

// Current returns the name of the active context.
func (s *Store) Current() string {
	return s.snap.Load().current
}

// Snapshot returns the configuration as of the last successful switch.
// The returned pointer must be treated as read-only.
func (s *Store) Snapshot() *Config {
	return s.snap.Load().config
}

// Switch atomically moves to another context.
//
// Concurrency: the new snapshot (config copy + new current name) is
// published with a single atomic store, so concurrent readers see
// either the complete old state or the complete new state.
//
// Durability: when a persist path is configured, the updated config is
// written via WriteFileAtomic (temp file + fsync + rename + dir fsync)
// before the new state becomes visible. If the process dies or the
// disk fills up mid-write, the file on disk is either the complete
// pre-switch or the complete post-switch content, and the in-memory
// state is only advanced after the write succeeded — so a failed
// Switch leaves memory and disk consistent with each other.
func (s *Store) Switch(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old := s.snap.Load()

	found := false
	for _, nc := range old.config.Contexts {
		if nc.Name == name {
			found = true
			break
		}
	}
	if !found {
		return &ErrContextNotFound{Name: name, Available: ContextNames(old.config)}
	}

	// Copy-on-write: never mutate the snapshot readers may hold.
	next := *old.config
	next.CurrentContext = name

	if s.path != "" {
		data, err := Marshal(&next)
		if err != nil {
			return err
		}
		if err := WriteFileAtomic(s.path, data); err != nil {
			return err
		}
	}

	s.snap.Store(&snapshot{config: &next, current: name})
	return nil
}
