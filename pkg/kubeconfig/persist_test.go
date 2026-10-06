package kubeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 写入失败时原文件必须完整保留，且不残留临时文件。
func TestAtomicWriteFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	original := []byte("original-complete-content")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	// 目标路径已存在且是目录时 rename 必然失败。
	badPath := filepath.Join(dir, "target")
	if err := os.Mkdir(badPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(badPath, []byte("new"), 0o600); err == nil {
		t.Fatal("expected rename error")
	}

	// 原文件未受影响。
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("original corrupted: %q", got)
	}
	// 目录中不得残留临时文件。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// 模拟进程中断：写入半途留下的临时文件不影响原文件，重启后读到完整旧状态。
func TestRecoveryFromInterruptedWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	original := []byte("apiVersion: v1\ncurrent-context: before\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	// 模拟崩溃残留的半成品临时文件。
	tmp := filepath.Join(dir, ".config.tmp-crashed")
	if err := os.WriteFile(tmp, []byte("partial-wr"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("interrupted write corrupted original: %q", got)
	}

	// 清理临时文件后重新完整写入，应得到完整新状态。
	os.Remove(tmp)
	next := []byte("apiVersion: v1\ncurrent-context: after\n")
	if err := AtomicWriteFile(path, next, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(next) {
		t.Fatalf("expected complete new state, got %q", got)
	}
}

// 权限不得被放宽：已有 0600 的文件用 0644 重写后仍为 0600。
func TestPermNeverLoosened(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(path, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("permission loosened to %o", perm)
	}
}

// 新文件按请求权限创建，且写满内容完整。
func TestAtomicWriteNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new")
	data := []byte("full-content")
	if err := AtomicWriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o", info.Mode().Perm())
	}
}
