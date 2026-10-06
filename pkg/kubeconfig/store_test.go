package kubeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func storeFixture(t *testing.T, dir string, contexts ...string) (*Store, string) {
	t.Helper()
	cfg := &Config{CurrentContext: contexts[0]}
	for _, n := range contexts {
		cfg.Contexts = append(cfg.Contexts, NamedContext{Name: n, Context: Context{Cluster: "c-" + n}})
		cfg.Clusters = append(cfg.Clusters, NamedCluster{Name: "c-" + n, Cluster: Cluster{Server: "https://" + n}})
	}
	path := filepath.Join(dir, "merged.yaml")
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, data); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(loaded, path), path
}

func TestSwitchUnknownContextFails(t *testing.T) {
	store, _ := storeFixture(t, t.TempDir(), "a", "b")
	err := store.Switch("nope")
	var notFound *ErrContextNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("expected ErrContextNotFound, got %v", err)
	}
	if notFound.Name != "nope" || len(notFound.Available) != 2 {
		t.Fatalf("bad diagnostic: %+v", notFound)
	}
	if store.Current() != "a" {
		t.Fatalf("failed switch changed state: %q", store.Current())
	}
}

func TestSwitchPersistsAtomicallyAndRecovers(t *testing.T) {
	dir := t.TempDir()
	store, path := storeFixture(t, dir, "alpha", "beta")

	if err := store.Switch("beta"); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if store.Current() != "beta" {
		t.Fatalf("current = %q", store.Current())
	}

	// "Restart": reload from disk; must show the post-switch state.
	reloaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("reload after switch: %v", err)
	}
	if reloaded.CurrentContext != "beta" {
		t.Fatalf("persisted current-context = %q, want beta", reloaded.CurrentContext)
	}
}

func TestFailedPersistLeavesConsistentState(t *testing.T) {
	dir := t.TempDir()
	store, path := storeFixture(t, dir, "alpha", "beta")
	before, _ := os.ReadFile(path)

	// Make persistence fail by removing the target directory's entry:
	// point the store at a path inside a now-deleted directory.
	broken := NewStore(store.Snapshot(), filepath.Join(dir, "deleted", "x.yaml"))
	if err := broken.Switch("beta"); err == nil {
		t.Fatal("expected persist failure")
	}
	if broken.Current() != "alpha" {
		t.Fatalf("in-memory state advanced despite failed persist: %q", broken.Current())
	}

	// The real store and its file are untouched.
	if store.Current() != "alpha" {
		t.Fatalf("state changed: %q", store.Current())
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("on-disk file modified by failed switch")
	}
}

// TestConcurrentSwitchAndRead hammers the store with concurrent
// switches and reads. The invariant: at any instant, the current
// context name and the snapshot config belong to the same switch
// generation — a reader must never observe a half-applied mix.
// Run with -race to also catch data races.
func TestConcurrentSwitchAndRead(t *testing.T) {
	dir := t.TempDir()
	names := []string{"c0", "c1", "c2", "c3"}
	store, path := storeFixture(t, dir, names...)

	const workers = 8
	const iterations = 200

	var wg sync.WaitGroup
	errCh := make(chan error, workers*iterations)

	// Writers: round-robin switches.
	for w := 0; w < workers/2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if err := store.Switch(names[(w+i)%len(names)]); err != nil {
					errCh <- fmt.Errorf("switch: %w", err)
					return
				}
			}
		}(w)
	}
	// Readers: verify snapshot self-consistency.
	for w := 0; w < workers/2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				cur := store.Current()
				snap := store.Snapshot()
				// The config's own current-context must agree with the
				// store's current name — they are published as one unit.
				if snap.CurrentContext != cur {
					errCh <- fmt.Errorf("mixed state: store=%q config=%q", cur, snap.CurrentContext)
					return
				}
				found := false
				for _, nc := range snap.Contexts {
					if nc.Name == cur {
						found = true
						break
					}
				}
				if !found {
					errCh <- fmt.Errorf("current %q missing from snapshot contexts", cur)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	// Final state on disk must equal final state in memory.
	reloaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.CurrentContext != store.Current() {
		t.Fatalf("disk=%q memory=%q after concurrent switches", reloaded.CurrentContext, store.Current())
	}
	if err := Validate(reloaded); err != nil {
		t.Fatalf("persisted config invalid: %v", err)
	}
}
