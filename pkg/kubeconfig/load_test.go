package kubeconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

// 相对路径的凭据字段必须解析到配置文件所在目录。
func TestLoadResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "certs", "ca.crt"), "CA", 0o600)
	writeFile(t, filepath.Join(dir, "certs", "client.crt"), "CRT", 0o600)
	writeFile(t, filepath.Join(dir, "certs", "client.key"), "KEY", 0o600)
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
clusters:
- name: c
  cluster:
    server: https://x
    certificate-authority: certs/ca.crt
users:
- name: u
  user:
    client-certificate: certs/client.crt
    client-key: certs/client.key
`, 0o600)

	cfg, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	wantCA := filepath.Join(dir, "certs", "ca.crt")
	if got := cfg.Clusters[0].Cluster.CertificateAuthority; got != wantCA {
		t.Fatalf("ca path = %q, want %q", got, wantCA)
	}
	if got := cfg.AuthInfos[0].AuthInfo.ClientKey; got != filepath.Join(dir, "certs", "client.key") {
		t.Fatalf("client key = %q", got)
	}
}

// 文件不存在要给出明确原因。
func TestLoadMissingFile(t *testing.T) {
	_, err := LoadFile(filepath.Join(t.TempDir(), "nope.yaml"))
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("expected LoadError, got %v", err)
	}
	if le.Err == nil || !errors.Is(le.Err, os.ErrNotExist) && !contains(le.Err.Error(), "no such file") {
		t.Fatalf("error should explain missing file: %v", le)
	}
}

// 不可读文件要给出权限相关原因（以非 root 身份运行时有效）。
func TestLoadUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	path := filepath.Join(t.TempDir(), "secret.yaml")
	writeFile(t, path, "apiVersion: v1", 0o000)
	_, err := LoadFile(path)
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("expected LoadError, got %v", err)
	}
	if !contains(le.Err.Error(), "permission denied") {
		t.Fatalf("error should mention permission: %v", le)
	}
}

// 引用的凭据文件不可读时要指明是哪个引用失败。
func TestLoadUnreadableReferencedCredential(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ca.crt"), "CA", 0o000)
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
clusters:
- name: c
  cluster:
    server: https://x
    certificate-authority: ca.crt
`, 0o600)
	_, err := LoadFile(cfgPath)
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("expected LoadError, got %v", err)
	}
	if !contains(err.Error(), "ca.crt") || !contains(err.Error(), "permission denied") {
		t.Fatalf("error should locate the referenced credential: %v", err)
	}
}

// 非法 YAML 与目录输入要报错。
func TestLoadMalformedInputs(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	writeFile(t, bad, "clusters: [unclosed", 0o600)
	if _, err := LoadFile(bad); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := LoadFile(dir); err == nil {
		t.Fatal("expected error for directory input")
	}
}

// 多文件按给定顺序加载，任一失败即报带定位的错误。
func TestLoadFilesOrderAndFailure(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.yaml")
	b := filepath.Join(dir, "b.yaml")
	writeFile(t, a, "current-context: from-a", 0o600)
	writeFile(t, b, "current-context: from-b", 0o600)

	cfgs, err := LoadFiles([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if cfgs[0].CurrentContext != "from-a" || cfgs[1].CurrentContext != "from-b" {
		t.Fatal("order not preserved")
	}
	if _, err := LoadFiles([]string{a, filepath.Join(dir, "missing.yaml")}); err == nil {
		t.Fatal("expected error for missing file in list")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
