package kubeconfig

// Override records that an entry from an earlier file was replaced by
// an entry of the same name from a later file. The full set of
// overrides makes the merge result explainable.
type Override struct {
	Section  string // "clusters", "users" or "contexts"
	Name     string
	FromFile string // file that previously defined the entry
	ToFile   string // file whose definition wins
}

// MergeReport describes what Merge did.
type MergeReport struct {
	// Files is the effective load order after normalization.
	Files []string
	// Overrides lists every entry that was replaced by a later file,
	// in the order the replacements happened.
	Overrides []Override
}

// Merge combines the given configs into one effective config.
// Later inputs override earlier ones for entries with the same name;
// the result is independent of map iteration or filesystem traversal
// order. Implementation to be filled in.
func Merge(configs ...*Config) (*Config, *MergeReport, error) {
	return nil, nil, nil
}
