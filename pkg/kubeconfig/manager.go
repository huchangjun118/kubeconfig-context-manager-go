package kubeconfig

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

// Manager 持有当前生效配置，支持并发安全的读取与上下文切换。
//
// 并发语义：Current/CurrentContext 通过原子快照读取，与 Switch 并发时
// 任一时刻读到的要么是完整的旧配置，要么是完整的新配置，
// 不会出现一半旧值一半新值的混合状态。
type Manager struct {
	mu      sync.Mutex  // 串行化 Switch，保证内存状态与磁盘状态一致
	current atomic.Value // 存储 *Config，快照不可变
}

// NewManager 基于初始配置创建 Manager。cfg 会被拷贝，之后外部修改不影响内部状态。
func NewManager(cfg *Config) *Manager {
	m := &Manager{}
	m.current.Store(deepCopyConfig(cfg))
	return m
}

// Current 返回当前自洽的配置快照。返回值为只读快照，调用方不得修改。
func (m *Manager) Current() *Config {
	return m.current.Load().(*Config)
}

// CurrentContext 返回当前生效的上下文条目。
func (m *Manager) CurrentContext() (*NamedContext, error) {
	cfg := m.Current()
	if cfg.CurrentContext == "" {
		return nil, fmt.Errorf("no current context set")
	}
	for i := range cfg.Contexts {
		if cfg.Contexts[i].Name == cfg.CurrentContext {
			ctx := cfg.Contexts[i]
			return &ctx, nil
		}
	}
	return nil, fmt.Errorf("current context %q not found among %d contexts",
		cfg.CurrentContext, len(cfg.Contexts))
}

// Switch 原子地切换到指定上下文，并把切换后的完整配置持久化到 path。
// 先持久化成功再更新内存快照：持久化失败时内存状态保持切换前的完整状态。
func (m *Manager) Switch(name, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := m.Current()
	found := false
	for i := range cfg.Contexts {
		if cfg.Contexts[i].Name == name {
			found = true
			break
		}
	}
	if !found {
		available := make([]string, 0, len(cfg.Contexts))
		for _, c := range cfg.Contexts {
			available = append(available, c.Name)
		}
		sort.Strings(available)
		return fmt.Errorf("context %q not found; available: %v", name, available)
	}

	next := deepCopyConfig(cfg)
	next.CurrentContext = name

	data, err := yaml.Marshal(next)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	// 敏感配置权限固定不超过 0600，已有更严格权限时保持更严格。
	if err := AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("persist switch to %q: %w", name, err)
	}
	m.current.Store(next)
	return nil
}

// deepCopyConfig 深拷贝配置，保证快照不可变、切换互不干扰。
func deepCopyConfig(c *Config) *Config {
	if c == nil {
		return &Config{}
	}
	out := *c
	out.Clusters = append([]NamedCluster(nil), c.Clusters...)
	out.AuthInfos = append([]NamedAuthInfo(nil), c.AuthInfos...)
	out.Contexts = append([]NamedContext(nil), c.Contexts...)
	if c.Extensions != nil {
		out.Extensions = make(map[string]yaml.Node, len(c.Extensions))
		for k, v := range c.Extensions {
			out.Extensions[k] = v
		}
	}
	return &out
}
