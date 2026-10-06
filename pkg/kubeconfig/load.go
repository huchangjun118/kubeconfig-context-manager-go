package kubeconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFile reads and parses a single kubeconfig file.
//
//   - Unreadable files (missing, permission denied, ...) fail with
//     ErrUnreadableInput wrapping the OS error, so callers can tell
//     "not found" apart from "not permitted".
//   - Relative credential paths (certificate-authority,
//     client-certificate, client-key, tokenFile, exec command) are
//     resolved against the directory containing the file, matching
//     kubectl semantics.
//   - Duplicate entry names inside the file fail with
//     ErrDuplicateEntry instead of silently picking one.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ErrUnreadableInput{File: path, Reason: err}
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, &ErrUnreadableInput{File: path, Reason: fmt.Errorf("invalid YAML: %w", err)}
	}
	cfg.path = path

	if err := checkDuplicates(sectionClusters, path, clusterNames(cfg)); err != nil {
		return nil, err
	}
	if err := checkDuplicates(sectionUsers, path, userNames(cfg)); err != nil {
		return nil, err
	}
	if err := checkDuplicates(sectionContexts, path, contextNames(cfg)); err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	for i := range cfg.Clusters {
		cfg.Clusters[i].Cluster.CertificateAuthority = resolvePath(dir, cfg.Clusters[i].Cluster.CertificateAuthority)
	}
	for i := range cfg.AuthInfos {
		a := &cfg.AuthInfos[i].AuthInfo
		a.ClientCertificate = resolvePath(dir, a.ClientCertificate)
		a.ClientKey = resolvePath(dir, a.ClientKey)
		a.TokenFile = resolvePath(dir, a.TokenFile)
		if a.Exec != nil {
			a.Exec.Command = resolvePath(dir, a.Exec.Command)
		}
	}
	return cfg, nil
}

// resolvePath anchors p at dir when p is a non-empty relative path.
// Absolute paths and empty strings are returned unchanged.
func resolvePath(dir, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Clean(filepath.Join(dir, p))
}

// LoadFiles reads several kubeconfig files and merges them in the
// given order. The paths are canonicalized (absolute, symlink-free)
// so that the merge result depends only on the caller-specified order,
// never on how the paths were spelled or discovered.
func LoadFiles(paths ...string) (*Config, *MergeReport, error) {
	if len(paths) == 0 {
		return nil, nil, errors.New("no kubeconfig files given")
	}
	configs := make([]*Config, 0, len(paths))
	canon := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, nil, &ErrUnreadableInput{File: p, Reason: err}
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		cfg, err := LoadFile(abs)
		if err != nil {
			return nil, nil, err
		}
		configs = append(configs, cfg)
		canon = append(canon, abs)
	}
	merged, report, err := Merge(configs...)
	if err != nil {
		return nil, nil, err
	}
	report.Files = canon
	return merged, report, nil
}

// Validate checks that every context points at an existing cluster and
// user. It returns the first dangling reference found, or nil.
func Validate(c *Config) error {
	clusters := make(map[string]struct{}, len(c.Clusters))
	for _, nc := range c.Clusters {
		clusters[nc.Name] = struct{}{}
	}
	users := make(map[string]struct{}, len(c.AuthInfos))
	for _, na := range c.AuthInfos {
		users[na.Name] = struct{}{}
	}
	for _, nc := range c.Contexts {
		if _, ok := clusters[nc.Context.Cluster]; !ok {
			return &ErrDanglingReference{Context: nc.Name, Section: "cluster", Name: nc.Context.Cluster}
		}
		if nc.Context.AuthInfo != "" {
			if _, ok := users[nc.Context.AuthInfo]; !ok {
				return &ErrDanglingReference{Context: nc.Name, Section: "user", Name: nc.Context.AuthInfo}
			}
		}
	}
	return nil
}

// ContextNames returns the sorted names of all contexts.
func ContextNames(c *Config) []string {
	names := make([]string, len(c.Contexts))
	for i := range c.Contexts {
		names[i] = c.Contexts[i].Name
	}
	sort.Strings(names)
	return names
}

// Marshal serializes a config. Unknown extension fields captured at
// load time are emitted verbatim (as yaml.Node subtrees), so content
// produced by newer tooling survives a read/write round-trip
// unmodified. Entry order is the config's own (already deterministic
// after Merge); top-level extension keys are emitted in sorted order.
func Marshal(c *Config) ([]byte, error) {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("marshal kubeconfig: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("marshal kubeconfig: %w", err)
	}
	return []byte(b.String()), nil
}

// WriteFileAtomic writes data to path atomically: the payload goes to
// a temp file in the same directory, is fsynced, then renamed over the
// target, and the directory is fsynced. A crash or full disk therefore
// leaves either the complete old file or the complete new file, never
// a truncated mix. The file is created with mode 0600 and an existing
// file's permissions are never widened.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup; on success the rename removes the name.
	defer os.Remove(tmpName)

	// Never widen permissions: if the target exists, keep its mode;
	// otherwise default to 0600 (kubeconfigs carry credentials).
	mode := fs.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		if mode&0o077 != 0 {
			// Tighten an existing group/world-readable file rather
			// than propagating loose permissions.
			mode &= 0o600
		}
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file over %s: %w", path, err)
	}
	// Persist the rename itself.
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
