package kubeconfig

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// MergeError 描述合并过程中发现的重复或冲突条目。
type MergeError struct {
	Kind  string // "cluster" / "user" / "context"
	Name  string
	Where string // 出错位置（文件名或来源序号）
	Err   error
}

func (e *MergeError) Error() string {
	return fmt.Sprintf("merge %s %q at %s: %v", e.Kind, e.Name, e.Where, e.Err)
}

func (e *MergeError) Unwrap() error { return e.Err }

// Source 把一份配置与其来源标识绑定，用于合并时的定位与解释。
type Source struct {
	Name   string // 通常是文件路径
	Config *Config
}

// MergeResult 是合并结果：生效配置 + 每个同名条目最终生效来源。
type MergeResult struct {
	Config *Config
	// Provenance 记录生效条目的来源：kind -> 条目名 -> 来源名。
	// 同名条目被覆盖时，这里记录的是最终生效（最后一次覆盖）的来源。
	Provenance map[string]map[string]string
	// CurrentContextFrom 记录 current-context 最终取值的来源。
	CurrentContextFrom string
}

// Explain 返回指定条目最终生效来源的可读说明。
func (r *MergeResult) Explain(kind, name string) string {
	if src, ok := r.Provenance[kind][name]; ok {
		return fmt.Sprintf("%s %q 生效值来自 %s", kind, name, src)
	}
	return fmt.Sprintf("%s %q 不存在于合并结果中", kind, name)
}

// MergeConfigs 按顺序合并多份配置，同名条目后者覆盖前者。
// 结果只取决于参数顺序，与文件系统遍历顺序无关。
func MergeConfigs(configs ...*Config) (*Config, error) {
	sources := make([]Source, len(configs))
	for i, c := range configs {
		sources[i] = Source{Name: fmt.Sprintf("config#%d", i), Config: c}
	}
	res, err := MergeSources(sources...)
	if err != nil {
		return nil, err
	}
	return res.Config, nil
}

// MergeSources 按给定顺序合并带来源标识的配置。
// 合并使用以名字为键的索引，条目规模增长时不会退化为两两比较。
func MergeSources(sources ...Source) (*MergeResult, error) {
	merged := &Config{}
	res := &MergeResult{
		Config: merged,
		Provenance: map[string]map[string]string{
			"cluster": {},
			"user":    {},
			"context": {},
		},
	}

	// 名字 -> 结果切片下标，保证 O(1) 查找与确定性覆盖。
	clusterIdx := map[string]int{}
	userIdx := map[string]int{}
	contextIdx := map[string]int{}

	for _, src := range sources {
		if src.Config == nil {
			return nil, &MergeError{Kind: "config", Where: src.Name, Err: fmt.Errorf("source is nil")}
		}
		cfg := src.Config

		// 标量字段：后者非空覆盖前者。
		if cfg.APIVersion != "" {
			merged.APIVersion = cfg.APIVersion
		}
		if cfg.Kind != "" {
			merged.Kind = cfg.Kind
		}
		if cfg.CurrentContext != "" {
			merged.CurrentContext = cfg.CurrentContext
			res.CurrentContextFrom = src.Name
		}
		// 顶层扩展字段：同名键后者覆盖前者，未识别内容完整保留。
		for k, v := range cfg.Extensions {
			if merged.Extensions == nil {
				merged.Extensions = map[string]yaml.Node{}
			}
			merged.Extensions[k] = v
		}

		seen := map[string]map[string]bool{
			"cluster": {},
			"user":    {},
			"context": {},
		}
		checkDup := func(kind, name string) error {
			if name == "" {
				return &MergeError{Kind: kind, Where: src.Name, Err: fmt.Errorf("entry with empty name")}
			}
			if seen[kind][name] {
				return &MergeError{Kind: kind, Name: name, Where: src.Name,
					Err: fmt.Errorf("duplicate entry within the same source")}
			}
			seen[kind][name] = true
			return nil
		}

		for _, nc := range cfg.Clusters {
			if err := checkDup("cluster", nc.Name); err != nil {
				return nil, err
			}
			if i, ok := clusterIdx[nc.Name]; ok {
				merged.Clusters[i] = nc // 后者覆盖前者，位置不变保证输出确定
			} else {
				clusterIdx[nc.Name] = len(merged.Clusters)
				merged.Clusters = append(merged.Clusters, nc)
			}
			res.Provenance["cluster"][nc.Name] = src.Name
		}
		for _, nu := range cfg.AuthInfos {
			if err := checkDup("user", nu.Name); err != nil {
				return nil, err
			}
			if i, ok := userIdx[nu.Name]; ok {
				merged.AuthInfos[i] = nu
			} else {
				userIdx[nu.Name] = len(merged.AuthInfos)
				merged.AuthInfos = append(merged.AuthInfos, nu)
			}
			res.Provenance["user"][nu.Name] = src.Name
		}
		for _, nx := range cfg.Contexts {
			if err := checkDup("context", nx.Name); err != nil {
				return nil, err
			}
			if i, ok := contextIdx[nx.Name]; ok {
				merged.Contexts[i] = nx
			} else {
				contextIdx[nx.Name] = len(merged.Contexts)
				merged.Contexts = append(merged.Contexts, nx)
			}
			res.Provenance["context"][nx.Name] = src.Name
		}
	}
	return res, nil
}
