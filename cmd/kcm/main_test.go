package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLIMergeUseCurrent(t *testing.T) {
	dir := t.TempDir()
	f1 := write(t, dir, "a.yaml", `
clusters: [{name: prod, cluster: {server: "https://v1:6443"}}]
users: [{name: admin, user: {token: t1}}]
contexts: [{name: prod, context: {cluster: prod, user: admin}}]
current-context: prod
`)
	f2 := write(t, dir, "b.yaml", `
clusters: [{name: prod, cluster: {server: "https://v2:6443"}}]
users: [{name: dev, user: {token: t2}}]
contexts: [{name: dev, context: {cluster: prod, user: dev}}]
`)
	merged := filepath.Join(dir, "merged.yaml")

	if err := run([]string{"merge", "-o", merged, f1, f2}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	data, _ := os.ReadFile(merged)
	if !strings.Contains(string(data), "https://v2:6443") {
		t.Fatalf("overlay did not win:\n%s", data)
	}

	if err := run([]string{"use", "-c", merged, "dev"}); err != nil {
		t.Fatalf("use: %v", err)
	}
	data, _ = os.ReadFile(merged)
	if !strings.Contains(string(data), "current-context: dev") {
		t.Fatalf("switch not persisted:\n%s", data)
	}

	if err := run([]string{"use", "-c", merged, "ghost"}); err == nil {
		t.Fatal("expected error switching to unknown context")
	}
}
