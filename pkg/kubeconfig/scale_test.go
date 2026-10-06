package kubeconfig

import (
	"fmt"
	"testing"
)

// TestMergeScalesLinearlyEnough merges several configs with thousands
// of entries each. The hash-indexed implementation handles this
// trivially; a pairwise (O(n^2)) implementation would not finish in
// reasonable time. Correctness of overrides is verified spot-wise.
func TestMergeScalesLinearlyEnough(t *testing.T) {
	const files = 3
	const perFile = 3000

	configs := make([]*Config, 0, files)
	for f := 0; f < files; f++ {
		c := &Config{path: fmt.Sprintf("f%d.yaml", f)}
		for i := 0; i < perFile; i++ {
			name := fmt.Sprintf("ctx-%05d", i)
			c.Contexts = append(c.Contexts, NamedContext{
				Name:    name,
				Context: Context{Cluster: name, Namespace: fmt.Sprintf("ns-f%d", f)},
			})
			c.Clusters = append(c.Clusters, NamedCluster{
				Name:    name,
				Cluster: Cluster{Server: fmt.Sprintf("https://%s-f%d:6443", name, f)},
			})
		}
		configs = append(configs, c)
	}

	merged, report, err := Merge(configs...)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(merged.Contexts) != perFile || len(merged.Clusters) != perFile {
		t.Fatalf("got %d contexts, %d clusters", len(merged.Contexts), len(merged.Clusters))
	}
	// Every entry overridden twice (f0 -> f1 -> f2).
	if len(report.Overrides) != 2*2*perFile {
		t.Fatalf("overrides = %d, want %d", len(report.Overrides), 4*perFile)
	}
	// Last file wins.
	if got := merged.Contexts[0].Context.Namespace; got != "ns-f2" {
		t.Fatalf("namespace = %q, want ns-f2", got)
	}
	if err := Validate(merged); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func BenchmarkMerge(b *testing.B) {
	const perFile = 1000
	mk := func(tag string) *Config {
		c := &Config{path: tag}
		for i := 0; i < perFile; i++ {
			name := fmt.Sprintf("ctx-%05d", i)
			c.Contexts = append(c.Contexts, NamedContext{Name: name, Context: Context{Cluster: name}})
			c.Clusters = append(c.Clusters, NamedCluster{Name: name, Cluster: Cluster{Server: "https://" + name}})
		}
		return c
	}
	a, bb, c := mk("a"), mk("b"), mk("c")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Merge(a, bb, c); err != nil {
			b.Fatal(err)
		}
	}
}
