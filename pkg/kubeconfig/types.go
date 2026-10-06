// Package kubeconfig implements deterministic merging of multiple
// kubeconfig files and safe, atomic context switching.
package kubeconfig

import "gopkg.in/yaml.v3"

// Config is the in-memory representation of a kubeconfig file.
// Unknown top-level fields are preserved in Extensions so that
// round-tripping a file never silently drops content produced by
// newer versions of the toolchain.
type Config struct {
	Kind           string               `yaml:"kind,omitempty"`
	APIVersion     string               `yaml:"apiVersion,omitempty"`
	Preferences    Preferences          `yaml:"preferences,omitempty"`
	Clusters       []NamedCluster       `yaml:"clusters,omitempty"`
	AuthInfos      []NamedAuthInfo      `yaml:"users,omitempty"`
	Contexts       []NamedContext       `yaml:"contexts,omitempty"`
	CurrentContext string               `yaml:"current-context,omitempty"`
	Extensions     map[string]yaml.Node `yaml:",inline"`

	// sources records, per section, which file each entry came from.
	// It is used to produce explainable override reports and precise
	// conflict diagnostics. Not serialized.
	sources map[string]map[string]string
}

// Preferences holds kubectl-style preferences. Unknown keys are kept.
type Preferences struct {
	Colors     bool                 `yaml:"colors,omitempty"`
	Extensions map[string]yaml.Node `yaml:",inline"`
}

// NamedCluster pairs a name with a Cluster.
type NamedCluster struct {
	Name    string  `yaml:"name"`
	Cluster Cluster `yaml:"cluster"`
}

// NamedAuthInfo pairs a name with an AuthInfo (user credentials).
type NamedAuthInfo struct {
	Name     string   `yaml:"name"`
	AuthInfo AuthInfo `yaml:"user"`
}

// NamedContext pairs a name with a Context.
type NamedContext struct {
	Name    string  `yaml:"name"`
	Context Context `yaml:"context"`
}

// Cluster describes how to reach an API server.
type Cluster struct {
	Server                   string `yaml:"server"`
	CertificateAuthority     string `yaml:"certificate-authority,omitempty"`
	CertificateAuthorityData []byte `yaml:"certificate-authority-data,omitempty"`
	InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify,omitempty"`
	TLSServerName            string `yaml:"tls-server-name,omitempty"`
	ProxyURL                 string `yaml:"proxy-url,omitempty"`
	DisableCompression       bool   `yaml:"disable-compression,omitempty"`

	Extensions map[string]yaml.Node `yaml:",inline"`
}

// AuthInfo describes credentials for a user.
type AuthInfo struct {
	ClientCertificate     string               `yaml:"client-certificate,omitempty"`
	ClientCertificateData []byte               `yaml:"client-certificate-data,omitempty"`
	ClientKey             string               `yaml:"client-key,omitempty"`
	ClientKeyData         []byte               `yaml:"client-key-data,omitempty"`
	Token                 string               `yaml:"token,omitempty"`
	TokenFile             string               `yaml:"tokenFile,omitempty"`
	Username              string               `yaml:"username,omitempty"`
	Password              string               `yaml:"password,omitempty"`
	AuthProvider          map[string]yaml.Node `yaml:"auth-provider,omitempty"`
	Exec                  *ExecConfig          `yaml:"exec,omitempty"`
	Impersonate           string               `yaml:"as,omitempty"`
	ImpersonateGroups     []string             `yaml:"as-groups,omitempty"`
	ImpersonateUserExtra  map[string]string    `yaml:"as-user-extra,omitempty"`

	Extensions map[string]yaml.Node `yaml:",inline"`
}

// ExecConfig describes an external credential plugin.
type ExecConfig struct {
	Command            string               `yaml:"command"`
	Args               []string             `yaml:"args,omitempty"`
	Env                []ExecEnvVar         `yaml:"env,omitempty"`
	APIVersion         string               `yaml:"apiVersion,omitempty"`
	InstallHint        string               `yaml:"installHint,omitempty"`
	ProvideClusterInfo bool                 `yaml:"provideClusterInfo,omitempty"`
	InteractiveMode    string               `yaml:"interactiveMode,omitempty"`
	Extensions         map[string]yaml.Node `yaml:",inline"`
}

// ExecEnvVar is an environment variable for an exec plugin.
type ExecEnvVar struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

// Context binds a cluster, a user and an optional namespace.
type Context struct {
	Cluster    string               `yaml:"cluster"`
	AuthInfo   string               `yaml:"user,omitempty"`
	Namespace  string               `yaml:"namespace,omitempty"`
	Extensions map[string]yaml.Node `yaml:",inline"`
}
