package kubeconfig

import (
	"path/filepath"
	"testing"
)

// 端到端：三份文件逐层覆盖，合并结果确定且可解释，切换后原子落盘。
func TestEndToEndMergeAndSwitch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "01-base.yaml"), `
current-context: dev
clusters:
- name: shared
  cluster: {server: https://base}
- name: base-only
  cluster: {server: https://base-only}
users:
- name: alice
  user: {token: base-token}
contexts:
- name: dev
  context: {cluster: shared, user: alice}
- name: prod
  context: {cluster: shared, user: alice, namespace: prod}
`, 0o600)
	writeFile(t, filepath.Join(dir, "02-team.yaml"), `
clusters:
- name: shared
  cluster: {server: https://team}
users:
- name: bob
  user: {token: team-token}
contexts:
- name: staging
  context: {cluster: shared, user: bob}
`, 0o600)
	writeFile(t, filepath.Join(dir, "03-local.yaml"), `
users:
- name: alice
  user: {token: local-token}
`, 0o600)

	files := []string{
		filepath.Join(dir, "01-base.yaml"),
		filepath.Join(dir, "02-team.yaml"),
		filepath.Join(dir, "03-local.yaml"),
	}
	cfgs, err := LoadFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	sources := make([]Source, len(files))
	for i, f := range files {
		sources[i] = Source{Name: f, Config: cfgs[i]}
	}
	res, err := MergeSources(sources...)
	if err != nil {
		t.Fatal(err)
	}
	m := res.Config

	// 覆盖关系：shared 集群取 team 值，alice 取 local 值，base-only 保留。
	servers := map[string]string{}
	for _, c := range m.Clusters {
		servers[c.Name] = c.Cluster.Server
	}
	if servers["shared"] != "https://team" || servers["base-only"] != "https://base-only" {
		t.Fatalf("unexpected clusters: %v", servers)
	}
	tokens := map[string]string{}
	for _, u := range m.AuthInfos {
		tokens[u.Name] = u.AuthInfo.Token
	}
	if tokens["alice"] != "local-token" || tokens["bob"] != "team-token" {
		t.Fatalf("unexpected users: %v", tokens)
	}
	if m.CurrentContext != "dev" {
		t.Fatalf("current-context = %q", m.CurrentContext)
	}
	if res.Provenance["cluster"]["shared"] != files[1] {
		t.Fatalf("provenance wrong: %v", res.Provenance["cluster"])
	}

	// 切换并持久化，重启（重新加载）后必须是完整的新状态。
	mgr := NewManager(m)
	out := filepath.Join(dir, "merged.yaml")
	if err := mgr.Switch("prod", out); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentContext != "prod" {
		t.Fatalf("reloaded current-context = %q", reloaded.CurrentContext)
	}
	if len(reloaded.Contexts) != 3 || len(reloaded.Clusters) != 2 || len(reloaded.AuthInfos) != 2 {
		t.Fatalf("reloaded config incomplete: %+v", reloaded)
	}
}
