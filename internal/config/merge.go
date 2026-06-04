package config

import "strings"

// ListMergeMode controls how a child profile's list field combines with
// the parent's list field during inheritance resolution. The default
// is "replace" — the child list completely overrides the parent. The
// other modes are opt-in and configured per-field via the
// `<field>-merge` sibling key in the YAML.
type ListMergeMode string

const (
	// MergeReplace is the default. Child list replaces parent list.
	MergeReplace ListMergeMode = "replace"
	// MergeAppend appends the child list to the parent list.
	// Example: parent=[A,B], child=[C] → [A,B,C]
	MergeAppend ListMergeMode = "append"
	// MergePrepend prepends the child list to the parent list.
	// Example: parent=[A,B], child=[C] → [C,A,B]
	MergePrepend ListMergeMode = "prepend"
	// MergeUnique appends the child list to the parent list and
	// removes duplicates (preserving first occurrence order).
	MergeUnique ListMergeMode = "unique"
)

// ParseListMergeMode maps a raw string to a ListMergeMode, returning
// MergeReplace for empty/unknown values. The check is case-insensitive
// because users will inevitably write "Append" or "APPEND".
func ParseListMergeMode(s string) ListMergeMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "replace":
		return MergeReplace
	case "append":
		return MergeAppend
	case "prepend":
		return MergePrepend
	case "unique":
		return MergeUnique
	default:
		return MergeReplace
	}
}

// mergeLists combines base and child according to the given mode. The
// returned slice is a fresh allocation; the inputs are not mutated.
func mergeLists(base, child []string, mode ListMergeMode) []string {
	switch mode {
	case MergeReplace:
		if child == nil {
			return nil
		}
		out := make([]string, len(child))
		copy(out, child)
		return out
	case MergeAppend:
		out := make([]string, 0, len(base)+len(child))
		out = append(out, base...)
		out = append(out, child...)
		return out
	case MergePrepend:
		out := make([]string, 0, len(base)+len(child))
		out = append(out, child...)
		out = append(out, base...)
		return out
	case MergeUnique:
		seen := make(map[string]struct{}, len(base)+len(child))
		out := make([]string, 0, len(base)+len(child))
		for _, s := range base {
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		for _, s := range child {
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		return out
	}
	return child
}
