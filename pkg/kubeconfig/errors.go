package kubeconfig

import "fmt"

// ErrDuplicateEntry reports the same entry name defined twice within a
// single kubeconfig file. It is distinct from cross-file overrides,
// which are legal and deterministic.
type ErrDuplicateEntry struct {
	Section string // "clusters", "users" or "contexts"
	Name    string
	File    string
}

func (e *ErrDuplicateEntry) Error() string {
	return fmt.Sprintf("kubeconfig %s: duplicate %s entry %q", e.File, e.Section, e.Name)
}

// ErrUnreadableInput reports an input file that cannot be read or
// parsed, with the underlying cause preserved.
type ErrUnreadableInput struct {
	File   string
	Reason error
}

func (e *ErrUnreadableInput) Error() string {
	return fmt.Sprintf("cannot read kubeconfig %s: %v", e.File, e.Reason)
}

func (e *ErrUnreadableInput) Unwrap() error { return e.Reason }

// ErrContextNotFound reports a switch request for a context that does
// not exist in the merged configuration.
type ErrContextNotFound struct {
	Name      string
	Available []string
}

func (e *ErrContextNotFound) Error() string {
	return fmt.Sprintf("context %q not found (available: %v)", e.Name, e.Available)
}

// ErrDanglingReference reports a context that points at a cluster or
// user that does not exist in the merged configuration.
type ErrDanglingReference struct {
	Context string
	Section string // "cluster" or "user"
	Name    string
}

func (e *ErrDanglingReference) Error() string {
	return fmt.Sprintf("context %q references unknown %s %q", e.Context, e.Section, e.Name)
}
