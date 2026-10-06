package kubeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testManagerConfig(n int) *Config {
	cfg := &Config{APIVersion: "v1", Kind: "Config", CurrentContext: "ctx-0"}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("ctx-%d", i)
		cfg.Contexts = append(cfg.Contexts, NamedContext{
			Name:    name,
			Context: Context{Cluster: "c-" + name, AuthInfo: "u-" + name},
		})
	}
	return cfg
}

// 并发切换与读取：任一时刻读到的 current-context 必须与其指向的
// 上下文条目自洽，不得出现混合状态。
func TestConcurrentSwitchAndRead(t *testing.T) {
	dir := t.TempDir()
	cfg := testManagerConfig(8)
	mgr := NewManager(cfg)

	var writers, readers sync.WaitGroup
	stop := make(chan struct{})

	// 多个写者并发切换到不同上下文（各自写不同文件，避免文件内容竞争）。
	for w := 0; w < 4; w++ {
		writers.Add(1)
		go func(w int) {
			defer writers.Done()
			path := filepath.Join(dir, fmt.Sprintf("out-%d.yaml", w))
			for i := 0; i < 200; i++ {
				name := fmt.Sprintf("ctx-%d", (w+i)%8)
				if err := mgr.Switch(name, path); err != nil {
					t.Errorf("switch: %v", err)
					return
				}
			}
		}(w)
	}

	// 多个读者持续读取，校验自洽性。
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap := mgr.Current()
				cur := snap.CurrentContext
				found := false
				for _, c := range snap.Contexts {
					if c.Name == cur {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("inconsistent snapshot: current-context %q not in contexts", cur)
					return
				}
				ctx, err := mgr.CurrentContext()
				if err != nil {
					t.Errorf("CurrentContext: %v", err)
					return
				}
				if ctx.Name == "" {
					t.Error("empty context name")
					return
				}
			}
		}()
	}

	writers.Wait()
	close(stop)
	readers.Wait()
}

// 持久化结果必须完整：切换后文件内容解析出的 current-context 与内存一致。
func TestSwitchPersistsCompleteState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	mgr := NewManager(testManagerConfig(4))

	if err := mgr.Switch("ctx-3", path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentContext != "ctx-3" {
		t.Fatalf("persisted current-context = %q", loaded.CurrentContext)
	}
	if mgr.Current().CurrentContext != "ctx-3" {
		t.Fatal("in-memory state not updated")
	}

	// 权限不得宽于 0600。
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("permission too loose: %o", perm)
	}
}

// 持久化失败时内存状态必须保持切换前的完整状态。
func TestSwitchPersistFailureKeepsOldState(t *testing.T) {
	mgr := NewManager(testManagerConfig(4))
	badPath := filepath.Join(t.TempDir(), "no-such-dir", "config.yaml")

	if err := mgr.Switch("ctx-2", badPath); err == nil {
		t.Fatal("expected persist error")
	}
	if got := mgr.Current().CurrentContext; got != "ctx-0" {
		t.Fatalf("state should stay at ctx-0 after failed persist, got %q", got)
	}
}

// 切换到不存在的上下文要报错并列出可选项。
func TestSwitchUnknownContext(t *testing.T) {
	mgr := NewManager(testManagerConfig(2))
	err := mgr.Switch("nope", filepath.Join(t.TempDir(), "c.yaml"))
	if err == nil {
		t.Fatal("expected error")
	}
	if got := mgr.Current().CurrentContext; got != "ctx-0" {
		t.Fatalf("state changed unexpectedly to %q", got)
	}
}
