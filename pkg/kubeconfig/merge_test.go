package kubeconfig

import (
	"errors"
	"testing"

	"gopkg.in/yaml.v3"
)

func cfgWith(path string, clusters, users, contexts []string, current string) *Config {
	c := &Config{path: path, CurrentContext: current}
	for _, n := range clusters {
		c.Clusters = append(c.Clusters, NamedCluster{Name: n, Cluster: Cluster{Server: "https://" + n + ":6443"}})
	}
	for _, n := range users {
		c.AuthInfos = append(c.AuthInfos, NamedAuthInfo{Name: n, AuthInfo: AuthInfo{Token: "tok-" + n}})
	}
	for _, n := range contexts {
		c.Contexts = append(c.Contexts, NamedContext{Name: n, Context: Context{Cluster: n, AuthInfo: n}})
	}
	return c
}

func TestMergeLaterFileWins(t *testing.T) {
	base := cfgWith("base.yaml", []string{"prod"}, []string{"admin"}, []string{"prod"}, "prod")
	overlay := cfgWith("overlay.yaml", nil, nil, nil, "")
	// Same cluster name, different server: overlay must win wholesale.
	overlay.Clusters = []NamedCluster{{Name: "prod", Cluster: Cluster{Server: "https://prod-v2:6443", TLSServerName: "prod.internal"}}}

	merged, report, err := Merge(base, overlay)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := merged.Clusters[0].Cluster.Server; got != "https://prod-v2:6443" {
		t.Fatalf("expected overlay server, got %q", got)
	}
	// Wholesale replacement: fields absent in the overlay must not leak
	// from the base entry.
	if got := merged.Clusters[0].Cluster.InsecureSkipTLSVerify; got {
		t.Fatal("expected wholesale replacement, got blended entry")
	}
	if len(report.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(report.Overrides))
	}
	ov := report.Overrides[0]
	if ov.Section != sectionClusters || ov.Name != "prod" || ov.FromFile != "base.yaml" || ov.ToFile != "overlay.yaml" {
		t.Fatalf("unexpected override record: %+v", ov)
	}
	if src := merged.SourceOf(sectionClusters, "prod"); src != "overlay.yaml" {
		t.Fatalf("SourceOf = %q, want overlay.yaml", src)
	}
}

func TestMergeOutputSortedAndDeterministic(t *testing.T) {
	a := cfgWith("a.yaml", []string{"zeta", "alpha", "mid"}, []string{"u2", "u1"}, []string{"c2", "c1"}, "")
	b := cfgWith("b.yaml", []string{"beta"}, nil, nil, "")

	merged1, _, err := Merge(a, b)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	merged2, _, err := Merge(a, b)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	wantOrder := []string{"alpha", "beta", "mid", "zeta"}
	if len(merged1.Clusters) != len(wantOrder) {
		t.Fatalf("got %d clusters", len(merged1.Clusters))
	}
	for i, want := range wantOrder {
		if merged1.Clusters[i].Name != want {
			t.Fatalf("cluster[%d] = %q, want %q", i, merged1.Clusters[i].Name, want)
		}
	}
	d1, _ := Marshal(merged1)
	d2, _ := Marshal(merged2)
	if string(d1) != string(d2) {
		t.Fatal("repeated merges produced different output")
	}
}

func TestMergeDuplicateWithinSingleFileFails(t *testing.T) {
	bad := cfgWith("bad.yaml", []string{"dup", "dup"}, nil, nil, "")
	_, _, err := Merge(bad)
	var dup *ErrDuplicateEntry
	if !errors.As(err, &dup) {
		t.Fatalf("expected ErrDuplicateEntry, got %v", err)
	}
	if dup.Section != sectionClusters || dup.Name != "dup" || dup.File != "bad.yaml" {
		t.Fatalf("incomplete diagnostic: %+v", dup)
	}
}

func TestMergeCurrentContextLastNonEmptyWins(t *testing.T) {
	a := cfgWith("a.yaml", nil, nil, nil, "from-a")
	b := cfgWith("b.yaml", nil, nil, nil, "")
	c := cfgWith("c.yaml", nil, nil, nil, "from-c")

	merged, _, err := Merge(a, b, c)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merged.CurrentContext != "from-c" {
		t.Fatalf("current-context = %q, want from-c", merged.CurrentContext)
	}

	merged, _, err = Merge(a, b)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merged.CurrentContext != "from-a" {
		t.Fatalf("empty current-context must not erase earlier value, got %q", merged.CurrentContext)
	}
}

func TestMergeExtensionCollisionReported(t *testing.T) {
	a := cfgWith("a.yaml", nil, nil, nil, "")
	b := cfgWith("b.yaml", nil, nil, nil, "")
	node := func(v string) yaml.Node {
		var n yaml.Node
		if err := n.Encode(v); err != nil {
			t.Fatalf("encode node: %v", err)
		}
		return n
	}
	a.Extensions = map[string]yaml.Node{"team": node("red")}
	b.Extensions = map[string]yaml.Node{"team": node("blue")}

	merged, report, err := Merge(a, b)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	ext := merged.Extensions["team"]
	var v string
	if err := ext.Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v != "blue" {
		t.Fatalf("extension = %q, want blue", v)
	}
	found := false
	for _, ov := range report.Overrides {
		if ov.Section == sectionExtensions && ov.Name == "team" {
			found = true
		}
	}
	if !found {
		t.Fatal("extension collision not reported")
	}
}
