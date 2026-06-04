package config

import "sort"

// sortStrings is a small local helper to keep the file free of any
// indirect dependency on the (already pulled in) "sort" package from the
// rest of the project.
func sortStrings(in []string) {
	sort.Strings(in)
}
