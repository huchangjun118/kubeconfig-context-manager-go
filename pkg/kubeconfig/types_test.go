package kubeconfig

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const richYAML = `
apiVersion: v1
kind: Config
current-context: prod
future-top-level: keep-me
preferences:
  colors: true
clusters:
- name: prod
  cluster:
    server: https://prod.example
    certificate-authority-data: Q0EtREFUQQ==
    future-cluster-field: 42
  cluster-entry-ext: abc
users:
- name: admin
  user:
    token: s3cret
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args: [eks, get-token]
contexts:
- name: prod
  context:
    cluster: prod
    user: admin
    namespace: default
    future-ctx: yes
`

func TestRoundTripPreservesExtensions(t *testing.T) {
	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(richYAML), cfg); err != nil {
		t.Fatal(err)
	}

	// 读取后扩展字段必须存在。
	if cfg.Extensions["future-top-level"].Value != "keep-me" {
		t.Fatalf("top-level extension lost: %+v", cfg.Extensions)
	}
	if _, ok := cfg.Extensions["preferences"]; !ok {
		t.Fatal("preferences extension lost")
	}
	if cfg.Clusters[0].Cluster.Extensions["future-cluster-field"].Value != "42" {
		t.Fatal("cluster extension lost")
	}
	if cfg.Clusters[0].Extensions["cluster-entry-ext"].Value != "abc" {
		t.Fatal("named cluster entry extension lost")
	}
	if _, ok := cfg.AuthInfos[0].AuthInfo.Extensions["exec"]; !ok {
		t.Fatal("authinfo exec extension lost")
	}
	if cfg.Contexts[0].Context.Extensions["future-ctx"].Value != "yes" {
		t.Fatal("context extension lost")
	}

	// 重新写出后扩展字段不得丢失、不得被改写。
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"future-top-level: keep-me",
		"colors: true",
		"future-cluster-field: 42",
		"cluster-entry-ext: abc",
		"command: aws",
		"future-ctx: yes",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// 二次读取应与第一次一致（幂等，无静默改写）。
	cfg2 := &Config{}
	if err := yaml.Unmarshal(out, cfg2); err != nil {
		t.Fatal(err)
	}
	out2, err := yaml.Marshal(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(out2) {
		t.Fatalf("round-trip not stable:\n%s\n---\n%s", out, out2)
	}
}

func TestKnownFieldsRoundTrip(t *testing.T) {
	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(richYAML), cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "prod" {
		t.Fatalf("current-context = %q", cfg.CurrentContext)
	}
	if string(cfg.Clusters[0].Cluster.CertificateAuthorityData) != "CA-DATA" {
		t.Fatalf("ca data decoded wrong: %q", cfg.Clusters[0].Cluster.CertificateAuthorityData)
	}
	if cfg.AuthInfos[0].AuthInfo.Token != "s3cret" {
		t.Fatal("token lost")
	}
	if cfg.Contexts[0].Context.Namespace != "default" {
		t.Fatal("namespace lost")
	}
}
