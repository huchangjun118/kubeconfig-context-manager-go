package kubeconfig

import (
	"errors"
	"testing"
)

func cfgWith(cluster, user, ctx, current string) *Config {
	c := &Config{APIVersion: "v1", Kind: "Config", CurrentContext: current}
	if cluster != "" {
		c.Clusters = []NamedCluster{{Name: cluster, Cluster: Cluster{Server: "https://" + cluster}}}
	}
	if user != "" {
		c.AuthInfos = []NamedAuthInfo{{Name: user, AuthInfo: AuthInfo{Token: "tok-" + user}}}
	}
	if ctx != "" {
		c.Contexts = []NamedContext{{Name: ctx, Context: Context{Cluster: cluster, AuthInfo: user}}}
	}
	return c
}

func TestMergeLaterOverridesEarlier(t *testing.T) {
	a := cfgWith("c1", "u1", "x", "x")
	a.Clusters[0].Cluster.Server = "https://old"
	b := cfgWith("c1", "u1", "x", "")
	b.Clusters[0].Cluster.Server = "https://new"

	merged, err := MergeConfigs(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.Clusters[0].Cluster.Server; got != "https://new" {
		t.Fatalf("later source should win, got %q", got)
	}
	if merged.CurrentContext != "x" {
		t.Fatalf("current-context from earlier source should be kept, got %q", merged.CurrentContext)
	}
}

func TestMergeOrderIndependentOfInputDelivery(t *testing.T) {
	// 同样的三份配置，无论以何种方式送达，只要合并顺序固定，结果必须一致。
	a := cfgWith("c1", "u1", "x", "x")
	b := cfgWith("c2", "u2", "y", "")
	c := cfgWith("c1", "", "", "y")
	c.Clusters[0].Cluster.Server = "https://c1-final"

	m1, err := MergeConfigs(a, b, c)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := MergeConfigs(a, b, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(m1.Clusters) != 2 || len(m2.Clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d/%d", len(m1.Clusters), len(m2.Clusters))
	}
	for i := range m1.Clusters {
		if m1.Clusters[i].Name != m2.Clusters[i].Name ||
			m1.Clusters[i].Cluster.Server != m2.Clusters[i].Cluster.Server {
			t.Fatalf("non-deterministic merge at %d", i)
		}
	}
	if m1.CurrentContext != "y" {
		t.Fatalf("current-context should be last non-empty, got %q", m1.CurrentContext)
	}
	// 覆盖后条目位置保持首次出现顺序，输出确定。
	if m1.Clusters[0].Name != "c1" || m1.Clusters[1].Name != "c2" {
		t.Fatalf("unexpected order: %v", m1.Clusters)
	}
	if m1.Clusters[0].Cluster.Server != "https://c1-final" {
		t.Fatalf("override not applied in place: %q", m1.Clusters[0].Cluster.Server)
	}
}

func TestMergeDuplicateWithinSameSourceFails(t *testing.T) {
	dup := cfgWith("c1", "", "", "")
	dup.Clusters = append(dup.Clusters, dup.Clusters[0])

	_, err := MergeSources(Source{Name: "dup.yaml", Config: dup})
	var me *MergeError
	if !errors.As(err, &me) {
		t.Fatalf("expected MergeError, got %v", err)
	}
	if me.Kind != "cluster" || me.Name != "c1" || me.Where != "dup.yaml" {
		t.Fatalf("error missing location info: %+v", me)
	}
}

func TestMergeEmptyNameFails(t *testing.T) {
	bad := &Config{Contexts: []NamedContext{{Name: ""}}}
	_, err := MergeSources(Source{Name: "bad.yaml", Config: bad})
	var me *MergeError
	if !errors.As(err, &me) || me.Where != "bad.yaml" {
		t.Fatalf("expected MergeError with location, got %v", err)
	}
}

func TestMergeProvenanceExplainsOverride(t *testing.T) {
	a := cfgWith("c1", "", "", "")
	b := cfgWith("c1", "", "", "")
	b.Clusters[0].Cluster.Server = "https://from-b"

	res, err := MergeSources(
		Source{Name: "base.yaml", Config: a},
		Source{Name: "override.yaml", Config: b},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Provenance["cluster"]["c1"]; got != "override.yaml" {
		t.Fatalf("provenance should record winner, got %q", got)
	}
	if res.Explain("cluster", "c1") == "" {
		t.Fatal("Explain should return non-empty message")
	}
	if got := res.Explain("cluster", "missing"); got == "" {
		t.Fatal("Explain for missing entry should still be readable")
	}
}
