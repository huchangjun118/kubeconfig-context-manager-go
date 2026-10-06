// Package kubeconfig 提供多份访问配置（kubeconfig）的合并、
// 上下文安全切换与原子持久化能力。
package kubeconfig

import (
	"encoding/base64"
	"fmt"

	"gopkg.in/yaml.v3"
)

// b64Data 处理 kubeconfig 中以 base64 字符串形式出现的字节字段
// （如 certificate-authority-data），读写保持 base64 字符串形态。
type b64Data []byte

func (b *b64Data) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	if s == "" {
		*b = nil
		return nil
	}
	d, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("invalid base64 data: %w", err)
	}
	*b = d
	return nil
}

func (b b64Data) MarshalYAML() (interface{}, error) {
	if len(b) == 0 {
		return "", nil
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Config 表示一份访问配置。
// 未识别的顶层字段保存在 Extensions 中，读写往返不丢失。
type Config struct {
	APIVersion     string
	Kind           string
	CurrentContext string
	Clusters       []NamedCluster
	AuthInfos      []NamedAuthInfo
	Contexts       []NamedContext
	Extensions     map[string]yaml.Node
}

// NamedCluster 是带名字的集群条目。
type NamedCluster struct {
	Name       string
	Cluster    Cluster
	Extensions map[string]yaml.Node
}

// Cluster 描述集群连接信息。
type Cluster struct {
	Server                   string
	CertificateAuthority     string
	CertificateAuthorityData []byte
	Extensions               map[string]yaml.Node
}

// NamedAuthInfo 是带名字的身份条目。
type NamedAuthInfo struct {
	Name     string
	AuthInfo AuthInfo
	// Extensions 保存条目级未识别字段。
	Extensions map[string]yaml.Node
}

// AuthInfo 描述身份凭据。
type AuthInfo struct {
	ClientCertificate     string
	ClientKey             string
	ClientCertificateData []byte
	ClientKeyData         []byte
	Token                 string
	Username              string
	Password              string
	Extensions            map[string]yaml.Node
}

// NamedContext 是带名字的上下文条目。
type NamedContext struct {
	Name       string
	Context    Context
	Extensions map[string]yaml.Node
}

// Context 把集群、身份与命名空间绑定为一个可切换单元。
type Context struct {
	Cluster   string
	AuthInfo  string
	Namespace string
	// Extensions 保存未识别字段。
	Extensions map[string]yaml.Node
}

// ---- 扩展字段保留的通用编解码辅助 ----

// decodeKnown 先把 node 解码到 known（含已知字段的 yaml 标签），
// 再把 node 中不属于 knownKeys 的键收集为 Extensions。
func decodeKnown(node *yaml.Node, known interface{}, knownKeys ...string) (map[string]yaml.Node, error) {
	if err := node.Decode(known); err != nil {
		return nil, err
	}
	var all map[string]yaml.Node
	if err := node.Decode(&all); err != nil {
		return nil, err
	}
	for _, k := range knownKeys {
		delete(all, k)
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}

// encodeWithExtensions 把已知字段（非零值）与扩展字段合并为一个 map 输出。
// 已知字段优先：扩展中若存在与已知字段同名的键会被忽略（正常流程下不会出现）。
func encodeWithExtensions(known map[string]interface{}, ext map[string]yaml.Node) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(known)+len(ext))
	for k, v := range ext {
		n := v // yaml.v3 编码器要求 *yaml.Node
		out[k] = &n
	}
	for k, v := range known {
		out[k] = v
	}
	return out, nil
}

// ---- Config ----

type rawConfig struct {
	APIVersion     string          `yaml:"apiVersion"`
	Kind           string          `yaml:"kind"`
	CurrentContext string          `yaml:"current-context"`
	Clusters       []NamedCluster  `yaml:"clusters"`
	AuthInfos      []NamedAuthInfo `yaml:"users"`
	Contexts       []NamedContext  `yaml:"contexts"`
}

var configKeys = []string{"apiVersion", "kind", "current-context", "clusters", "users", "contexts"}

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	var raw rawConfig
	ext, err := decodeKnown(node, &raw, configKeys...)
	if err != nil {
		return err
	}
	c.APIVersion = raw.APIVersion
	c.Kind = raw.Kind
	c.CurrentContext = raw.CurrentContext
	c.Clusters = raw.Clusters
	c.AuthInfos = raw.AuthInfos
	c.Contexts = raw.Contexts
	c.Extensions = ext
	return nil
}

func (c Config) MarshalYAML() (interface{}, error) {
	known := map[string]interface{}{}
	if c.APIVersion != "" {
		known["apiVersion"] = c.APIVersion
	}
	if c.Kind != "" {
		known["kind"] = c.Kind
	}
	if c.CurrentContext != "" {
		known["current-context"] = c.CurrentContext
	}
	if len(c.Clusters) > 0 {
		known["clusters"] = c.Clusters
	}
	if len(c.AuthInfos) > 0 {
		known["users"] = c.AuthInfos
	}
	if len(c.Contexts) > 0 {
		known["contexts"] = c.Contexts
	}
	return encodeWithExtensions(known, c.Extensions)
}

// ---- NamedCluster / Cluster ----

type rawNamedCluster struct {
	Name    string  `yaml:"name"`
	Cluster Cluster `yaml:"cluster"`
}

func (n *NamedCluster) UnmarshalYAML(node *yaml.Node) error {
	var raw rawNamedCluster
	ext, err := decodeKnown(node, &raw, "name", "cluster")
	if err != nil {
		return err
	}
	n.Name, n.Cluster, n.Extensions = raw.Name, raw.Cluster, ext
	return nil
}

func (n NamedCluster) MarshalYAML() (interface{}, error) {
	return encodeWithExtensions(map[string]interface{}{
		"name":    n.Name,
		"cluster": n.Cluster,
	}, n.Extensions)
}

type rawCluster struct {
	Server                   string  `yaml:"server"`
	CertificateAuthority     string  `yaml:"certificate-authority"`
	CertificateAuthorityData b64Data `yaml:"certificate-authority-data"`
}

func (c *Cluster) UnmarshalYAML(node *yaml.Node) error {
	var raw rawCluster
	ext, err := decodeKnown(node, &raw, "server", "certificate-authority", "certificate-authority-data")
	if err != nil {
		return err
	}
	c.Server = raw.Server
	c.CertificateAuthority = raw.CertificateAuthority
	c.CertificateAuthorityData = raw.CertificateAuthorityData
	c.Extensions = ext
	return nil
}

func (c Cluster) MarshalYAML() (interface{}, error) {
	known := map[string]interface{}{}
	if c.Server != "" {
		known["server"] = c.Server
	}
	if c.CertificateAuthority != "" {
		known["certificate-authority"] = c.CertificateAuthority
	}
	if len(c.CertificateAuthorityData) > 0 {
		known["certificate-authority-data"] = b64Data(c.CertificateAuthorityData)
	}
	return encodeWithExtensions(known, c.Extensions)
}

// ---- NamedAuthInfo / AuthInfo ----

type rawNamedAuthInfo struct {
	Name     string   `yaml:"name"`
	AuthInfo AuthInfo `yaml:"user"`
}

func (n *NamedAuthInfo) UnmarshalYAML(node *yaml.Node) error {
	var raw rawNamedAuthInfo
	ext, err := decodeKnown(node, &raw, "name", "user")
	if err != nil {
		return err
	}
	n.Name, n.AuthInfo, n.Extensions = raw.Name, raw.AuthInfo, ext
	return nil
}

func (n NamedAuthInfo) MarshalYAML() (interface{}, error) {
	return encodeWithExtensions(map[string]interface{}{
		"name": n.Name,
		"user": n.AuthInfo,
	}, n.Extensions)
}

type rawAuthInfo struct {
	ClientCertificate     string  `yaml:"client-certificate"`
	ClientKey             string  `yaml:"client-key"`
	ClientCertificateData b64Data `yaml:"client-certificate-data"`
	ClientKeyData         b64Data `yaml:"client-key-data"`
	Token                 string  `yaml:"token"`
	Username              string  `yaml:"username"`
	Password              string  `yaml:"password"`
}

var authInfoKeys = []string{
	"client-certificate", "client-key",
	"client-certificate-data", "client-key-data",
	"token", "username", "password",
}

func (a *AuthInfo) UnmarshalYAML(node *yaml.Node) error {
	var raw rawAuthInfo
	ext, err := decodeKnown(node, &raw, authInfoKeys...)
	if err != nil {
		return err
	}
	*a = AuthInfo{
		ClientCertificate:     raw.ClientCertificate,
		ClientKey:             raw.ClientKey,
		ClientCertificateData: raw.ClientCertificateData,
		ClientKeyData:         raw.ClientKeyData,
		Token:                 raw.Token,
		Username:              raw.Username,
		Password:              raw.Password,
		Extensions:            ext,
	}
	return nil
}

func (a AuthInfo) MarshalYAML() (interface{}, error) {
	known := map[string]interface{}{}
	if a.ClientCertificate != "" {
		known["client-certificate"] = a.ClientCertificate
	}
	if a.ClientKey != "" {
		known["client-key"] = a.ClientKey
	}
	if len(a.ClientCertificateData) > 0 {
		known["client-certificate-data"] = b64Data(a.ClientCertificateData)
	}
	if len(a.ClientKeyData) > 0 {
		known["client-key-data"] = b64Data(a.ClientKeyData)
	}
	if a.Token != "" {
		known["token"] = a.Token
	}
	if a.Username != "" {
		known["username"] = a.Username
	}
	if a.Password != "" {
		known["password"] = a.Password
	}
	return encodeWithExtensions(known, a.Extensions)
}

// ---- NamedContext / Context ----

type rawNamedContext struct {
	Name    string  `yaml:"name"`
	Context Context `yaml:"context"`
}

func (n *NamedContext) UnmarshalYAML(node *yaml.Node) error {
	var raw rawNamedContext
	ext, err := decodeKnown(node, &raw, "name", "context")
	if err != nil {
		return err
	}
	n.Name, n.Context, n.Extensions = raw.Name, raw.Context, ext
	return nil
}

func (n NamedContext) MarshalYAML() (interface{}, error) {
	return encodeWithExtensions(map[string]interface{}{
		"name":    n.Name,
		"context": n.Context,
	}, n.Extensions)
}

type rawContext struct {
	Cluster   string `yaml:"cluster"`
	AuthInfo  string `yaml:"user"`
	Namespace string `yaml:"namespace"`
}

func (c *Context) UnmarshalYAML(node *yaml.Node) error {
	var raw rawContext
	ext, err := decodeKnown(node, &raw, "cluster", "user", "namespace")
	if err != nil {
		return err
	}
	c.Cluster, c.AuthInfo, c.Namespace = raw.Cluster, raw.AuthInfo, raw.Namespace
	c.Extensions = ext
	return nil
}

func (c Context) MarshalYAML() (interface{}, error) {
	known := map[string]interface{}{}
	if c.Cluster != "" {
		known["cluster"] = c.Cluster
	}
	if c.AuthInfo != "" {
		known["user"] = c.AuthInfo
	}
	if c.Namespace != "" {
		known["namespace"] = c.Namespace
	}
	return encodeWithExtensions(known, c.Extensions)
}
