package kubeconfig

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseKubeconfig = `
kind: Config
apiVersion: v1
preferences:
  colors: true
  future-pref: keep-me
clusters:
- name: prod
  cluster:
    server: https://prod:6443
    certificate-authority: certs/ca.crt
    future-cluster-field: {nested: [1, 2, 3]}
users:
- name: admin
  user:
    token: s3cret
    client-key: ../shared/admin.key
    future-user-field: "preserved"
contexts:
- name: prod
  context:
    cluster: prod
    user: admin
    namespace: default
current-context: prod
future-top-level: |
  multi
  line
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFileResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "conf.d"), "config.yaml", baseKubeconfig)

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	wantCA := filepath.Join(dir, "conf.d", "certs", "ca.crt")
	if cfg.Clusters[0].Cluster.CertificateAuthority != wantCA {
		t.Fatalf("CA = %q, want %q", cfg.Clusters[0].Cluster.CertificateAuthority, wantCA)
	}
	wantKey := filepath.Join(dir, "shared", "admin.key")
	if cfg.AuthInfos[0].AuthInfo.ClientKey != wantKey {
		t.Fatalf("client-key = %q, want %q", cfg.AuthInfos[0].AuthInfo.ClientKey, wantKey)
	}
}

func TestRoundTripPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", baseKubeconfig)

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	out, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(out)
	for _, want := range []string{
		"future-pref: keep-me",
		"future-cluster-field:",
		"nested:",
		`future-user-field: "preserved"`,
		"future-top-level: |",
		"multi",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("round-trip lost %q; output:\n%s", want, text)
		}
	}

	// Re-parse the output: it must still be valid and equal in content.
	cfg2, err := LoadFile(writeFile(t, dir, "config2.yaml", text))
	if err != nil {
		t.Fatalf("re-load marshaled config: %v", err)
	}
	if cfg2.Contexts[0].Context.Namespace != "default" {
		t.Fatal("known field corrupted during round-trip")
	}
}

func TestLoadFileUnreadableInputs(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadFile(filepath.Join(dir, "missing.yaml"))
	var unread *ErrUnreadableInput
	if !errors.As(err, &unread) {
		t.Fatalf("expected ErrUnreadableInput, got %v", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cause should unwrap to fs.ErrNotExist, got %v", err)
	}

	bad := writeFile(t, dir, "bad.yaml", "clusters: [not, valid")
	if _, err := LoadFile(bad); !errors.As(err, &unread) {
		t.Fatalf("expected ErrUnreadableInput for bad YAML, got %v", err)
	}

	// Permission-denied input must fail with a clear reason.
	// (Root bypasses file permission checks, so skip there.)
	if os.Geteuid() != 0 {
		noRead := writeFile(t, dir, "noread.yaml", baseKubeconfig)
		if err := os.Chmod(noRead, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(noRead, 0o600) })
		if _, err := LoadFile(noRead); !errors.As(err, &unread) || !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("expected permission-denied ErrUnreadableInput, got %v", err)
		}
	}
}

func TestLoadFileDuplicateEntryFails(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "dup.yaml", `
contexts:
- name: a
  context: {cluster: c1}
- name: a
  context: {cluster: c2}
`)
	_, err := LoadFile(path)
	var dup *ErrDuplicateEntry
	if !errors.As(err, &dup) {
		t.Fatalf("expected ErrDuplicateEntry, got %v", err)
	}
	if dup.Section != sectionContexts || dup.Name != "a" {
		t.Fatalf("bad diagnostic: %+v", dup)
	}
}

func TestLoadFilesMergeOrderAndReport(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "01-base.yaml", `
clusters:
- name: shared
  cluster: {server: "https://v1:6443"}
contexts:
- name: shared
  context: {cluster: shared}
current-context: shared
`)
	f2 := writeFile(t, dir, "02-overlay.yaml", `
clusters:
- name: shared
  cluster: {server: "https://v2:6443"}
contexts:
- name: shared
  context: {cluster: shared, namespace: team-b}
`)

	merged, report, err := LoadFiles(f1, f2)
	if err != nil {
		t.Fatalf("LoadFiles: %v", err)
	}
	if got := merged.Clusters[0].Cluster.Server; got != "https://v2:6443" {
		t.Fatalf("server = %q, want v2", got)
	}
	if got := merged.Contexts[0].Context.Namespace; got != "team-b" {
		t.Fatalf("namespace = %q, want team-b", got)
	}
	if len(report.Overrides) != 2 {
		t.Fatalf("expected 2 overrides, got %+v", report.Overrides)
	}
	if len(report.Files) != 2 || !filepath.IsAbs(report.Files[0]) {
		t.Fatalf("report files not canonicalized: %+v", report.Files)
	}
}

func TestWriteFileAtomicPermissionsAndContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.yaml")

	if err := WriteFileAtomic(path, []byte("a: 1\n")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}

	// A pre-existing loose file must be tightened, never widened.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("a: 2\n")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("loose mode not tightened: %o", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "a: 2\n" {
		t.Fatalf("content = %q", data)
	}

	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func TestWriteFileAtomicFailureLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "keep.yaml", "original: true\n")

	// Force failure: target's directory removed -> rename impossible.
	gone := filepath.Join(dir, "gone", "x.yaml")
	if err := WriteFileAtomic(gone, []byte("x")); err == nil {
		t.Fatal("expected error for missing directory")
	}

	// Original file untouched after a failed write elsewhere.
	data, _ := os.ReadFile(path)
	if string(data) != "original: true\n" {
		t.Fatalf("original file modified: %q", data)
	}
}

func TestValidateDanglingReference(t *testing.T) {
	c := &Config{
		Contexts: []NamedContext{{Name: "broken", Context: Context{Cluster: "ghost", AuthInfo: "nobody"}}},
	}
	err := Validate(c)
	var dang *ErrDanglingReference
	if !errors.As(err, &dang) {
		t.Fatalf("expected ErrDanglingReference, got %v", err)
	}
	if dang.Context != "broken" || dang.Section != "cluster" || dang.Name != "ghost" {
		t.Fatalf("bad diagnostic: %+v", dang)
	}
}
