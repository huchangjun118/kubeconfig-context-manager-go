package kubeconfig

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// Override records that an entry from an earlier file was replaced by
// an entry of the same name from a later file. The full set of
// overrides makes the merge result explainable.
type Override struct {
	Section  string // "clusters", "users", "contexts" or "extensions"
	Name     string
	FromFile string // file that previously defined the entry
	ToFile   string // file whose definition wins
}

// MergeReport describes what Merge did.
type MergeReport struct {
	// Files is the effective load order.
	Files []string
	// Overrides lists every entry that was replaced by a later file,
	// in the order the replacements happened.
	Overrides []Override
}

// section names used in reports, diagnostics and source tracking.
const (
	sectionClusters   = "clusters"
	sectionUsers      = "users"
	sectionContexts   = "contexts"
	sectionExtensions = "extensions"
)

// Merge combines the given configs into one effective config.
//
// Rules (deterministic, order-only):
//   - Entries are keyed by name. For equal names, the entry from the
//     later input replaces the earlier one wholesale (no field-level
//     blending), and the replacement is recorded in the report.
//   - Duplicate names inside a single input are an error
//     (ErrDuplicateEntry), never a silent pick-one.
//   - Output entries are sorted by name, so the result depends only on
//     the input order, never on map iteration or filesystem traversal
//     order.
//   - Unknown extension fields are carried through untouched; on key
//     collision the later input's value wins and is reported.
//
// Merge and lookup are hash-index based: O(total entries), never
// pairwise comparison.
func Merge(configs ...*Config) (*Config, *MergeReport, error) {
	out := &Config{
		sources: map[string]map[string]string{
			sectionClusters:   {},
			sectionUsers:      {},
			sectionContexts:   {},
			sectionExtensions: {},
		},
	}
	report := &MergeReport{}

	// name -> position in the output slice, per section.
	clusterIdx := map[string]int{}
	userIdx := map[string]int{}
	contextIdx := map[string]int{}

	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		file := cfg.filePath()
		report.Files = append(report.Files, file)

		// Duplicate names within one file are a hard error with a
		// precise location; across files they are legal overrides.
		if err := checkDuplicates(sectionClusters, file, clusterNames(cfg)); err != nil {
			return nil, nil, err
		}
		if err := checkDuplicates(sectionUsers, file, userNames(cfg)); err != nil {
			return nil, nil, err
		}
		if err := checkDuplicates(sectionContexts, file, contextNames(cfg)); err != nil {
			return nil, nil, err
		}

		for _, nc := range cfg.Clusters {
			if pos, exists := clusterIdx[nc.Name]; exists {
				report.Overrides = append(report.Overrides, Override{
					Section:  sectionClusters,
					Name:     nc.Name,
					FromFile: out.sources[sectionClusters][nc.Name],
					ToFile:   file,
				})
				out.Clusters[pos] = nc
			} else {
				clusterIdx[nc.Name] = len(out.Clusters)
				out.Clusters = append(out.Clusters, nc)
			}
			out.sources[sectionClusters][nc.Name] = file
		}

		for _, na := range cfg.AuthInfos {
			if pos, exists := userIdx[na.Name]; exists {
				report.Overrides = append(report.Overrides, Override{
					Section:  sectionUsers,
					Name:     na.Name,
					FromFile: out.sources[sectionUsers][na.Name],
					ToFile:   file,
				})
				out.AuthInfos[pos] = na
			} else {
				userIdx[na.Name] = len(out.AuthInfos)
				out.AuthInfos = append(out.AuthInfos, na)
			}
			out.sources[sectionUsers][na.Name] = file
		}

		for _, nc := range cfg.Contexts {
			if pos, exists := contextIdx[nc.Name]; exists {
				report.Overrides = append(report.Overrides, Override{
					Section:  sectionContexts,
					Name:     nc.Name,
					FromFile: out.sources[sectionContexts][nc.Name],
					ToFile:   file,
				})
				out.Contexts[pos] = nc
			} else {
				contextIdx[nc.Name] = len(out.Contexts)
				out.Contexts = append(out.Contexts, nc)
			}
			out.sources[sectionContexts][nc.Name] = file
		}

		// Scalar fields: later files win when they set a value.
		if cfg.CurrentContext != "" {
			out.CurrentContext = cfg.CurrentContext
		}
		if cfg.Kind != "" {
			out.Kind = cfg.Kind
		}
		if cfg.APIVersion != "" {
			out.APIVersion = cfg.APIVersion
		}
		if cfg.Preferences.Colors {
			out.Preferences.Colors = true
		}
		if len(cfg.Preferences.Extensions) > 0 && out.Preferences.Extensions == nil {
			out.Preferences.Extensions = map[string]yaml.Node{}
		}
		for k, v := range cfg.Preferences.Extensions {
			out.Preferences.Extensions[k] = v
		}

		// Top-level unknown fields are preserved; later files win on
		// key collision and the win is reported for explainability.
		keys := make([]string, 0, len(cfg.Extensions))
		for k := range cfg.Extensions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if out.Extensions == nil {
				out.Extensions = map[string]yaml.Node{}
			}
			if prev, exists := out.sources[sectionExtensions][k]; exists {
				report.Overrides = append(report.Overrides, Override{
					Section:  sectionExtensions,
					Name:     k,
					FromFile: prev,
					ToFile:   file,
				})
			}
			out.Extensions[k] = cfg.Extensions[k]
			out.sources[sectionExtensions][k] = file
		}
	}

	// Deterministic output order: sort every section by name.
	sort.Slice(out.Clusters, func(i, j int) bool { return out.Clusters[i].Name < out.Clusters[j].Name })
	sort.Slice(out.AuthInfos, func(i, j int) bool { return out.AuthInfos[i].Name < out.AuthInfos[j].Name })
	sort.Slice(out.Contexts, func(i, j int) bool { return out.Contexts[i].Name < out.Contexts[j].Name })

	return out, report, nil
}

func checkDuplicates(section, file string, names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		if _, dup := seen[n]; dup {
			return &ErrDuplicateEntry{Section: section, Name: n, File: file}
		}
		seen[n] = struct{}{}
	}
	return nil
}

func clusterNames(c *Config) []string {
	names := make([]string, len(c.Clusters))
	for i := range c.Clusters {
		names[i] = c.Clusters[i].Name
	}
	return names
}

func userNames(c *Config) []string {
	names := make([]string, len(c.AuthInfos))
	for i := range c.AuthInfos {
		names[i] = c.AuthInfos[i].Name
	}
	return names
}

func contextNames(c *Config) []string {
	names := make([]string, len(c.Contexts))
	for i := range c.Contexts {
		names[i] = c.Contexts[i].Name
	}
	return names
}
