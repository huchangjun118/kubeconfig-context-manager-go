package kubeconfig

// LoadFile reads and parses a single kubeconfig file. Relative paths
// for credentials are resolved against the directory containing the
// file. Implementation to be filled in.
func LoadFile(path string) (*Config, error) {
	return nil, nil
}

// LoadFiles reads several kubeconfig files and merges them in the
// given order. Implementation to be filled in.
func LoadFiles(paths ...string) (*Config, *MergeReport, error) {
	return nil, nil, nil
}

// Marshal serializes a config, preserving unknown extension fields
// exactly as they were read.
func Marshal(c *Config) ([]byte, error) {
	return nil, nil
}
