package kubeconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadError 描述读取单份配置失败的原因，带文件定位信息。
type LoadError struct {
	Path string
	Err  error
}

func (e *LoadError) Error() string { return fmt.Sprintf("load %s: %v", e.Path, e.Err) }
func (e *LoadError) Unwrap() error { return e.Err }

// LoadFile 读取并解析单份配置文件。
// 相对路径的凭据字段会被解析为相对于该文件所在目录的绝对路径；
// 文件不存在、不可读、越权或引用的凭据文件不可读时返回带定位的错误。
func LoadFile(path string) (*Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, &LoadError{Path: path, Err: explainFSError(err, "stat")}
	}
	if !info.Mode().IsRegular() {
		return nil, &LoadError{Path: path, Err: fmt.Errorf("not a regular file (mode %s)", info.Mode())}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &LoadError{Path: path, Err: explainFSError(err, "read")}
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, &LoadError{Path: path, Err: fmt.Errorf("parse YAML: %w", err)}
	}

	dir := filepath.Dir(path)
	for i := range cfg.Clusters {
		c := &cfg.Clusters[i].Cluster
		c.CertificateAuthority = resolvePath(dir, c.CertificateAuthority)
		if err := checkReadable(c.CertificateAuthority, path); err != nil {
			return nil, err
		}
	}
	for i := range cfg.AuthInfos {
		a := &cfg.AuthInfos[i].AuthInfo
		a.ClientCertificate = resolvePath(dir, a.ClientCertificate)
		a.ClientKey = resolvePath(dir, a.ClientKey)
		if err := checkReadable(a.ClientCertificate, path); err != nil {
			return nil, err
		}
		if err := checkReadable(a.ClientKey, path); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// LoadFiles 按给定顺序读取多份配置文件，任一失败即返回带定位的错误。
func LoadFiles(paths []string) ([]*Config, error) {
	configs := make([]*Config, 0, len(paths))
	for _, p := range paths {
		cfg, err := LoadFile(p)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

// resolvePath 把相对路径解析为相对于 baseDir 的绝对路径；空值与绝对路径原样返回。
func resolvePath(baseDir, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}

// checkReadable 校验被引用的凭据文件存在且当前进程可读。
func checkReadable(ref, referrer string) error {
	if ref == "" {
		return nil
	}
	f, err := os.Open(ref)
	if err != nil {
		return &LoadError{Path: referrer,
			Err: fmt.Errorf("referenced credential %s: %w", ref, explainFSError(err, "open"))}
	}
	f.Close()
	return nil
}

// explainFSError 把底层文件系统错误翻译为明确的失败原因。
func explainFSError(err error, op string) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s: no such file or directory", op)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%s: permission denied", op)
	default:
		return err
	}
}
