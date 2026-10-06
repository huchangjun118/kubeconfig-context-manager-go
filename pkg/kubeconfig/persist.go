package kubeconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteFile 原子地把 data 写入 path：
//  1. 在同目录创建临时文件（权限 0600），写入并 fsync；
//  2. 将权限调整为 effectivePerm(path, perm)——绝不比已有文件更宽松；
//  3. rename 覆盖目标，并 fsync 父目录。
//
// 任意时刻进程中断或磁盘写满，目标路径要么保留完整的旧内容，
// 要么是完整的新内容，不会留下损坏文件；临时文件会被清理。
func AtomicWriteFile(path string, data []byte, perm uint32) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// 任何失败路径都清理临时文件。
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("fsync temp file: %w", err)
	}

	effective := effectivePerm(path, perm)
	if err := tmp.Chmod(effective); err != nil {
		return fmt.Errorf("chmod temp file to %o: %w", effective, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}

	// fsync 父目录，保证 rename 结果落盘，重启后状态可恢复。
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir %s: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync dir %s: %w", dir, err)
	}
	return nil
}

// effectivePerm 计算最终权限：取请求权限与已有文件权限中更严格的一方，
// 保证敏感内容的权限不会被放宽。
func effectivePerm(path string, requested uint32) os.FileMode {
	eff := os.FileMode(requested) & os.ModePerm
	if info, err := os.Stat(path); err == nil {
		existing := info.Mode().Perm()
		// 按位与：任一一方不允许的权限位都不会出现在结果里。
		eff &= existing
	}
	if eff == 0 {
		eff = 0o600
	}
	return eff
}
