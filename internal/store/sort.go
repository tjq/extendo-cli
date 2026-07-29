package store

import "slices"

// Sorted returns items ordered the way the popup shows them: pinned entries
// first, then newest first. The sort is stable, so items sharing a timestamp
// keep their original relative order.
func Sorted(items []Item) []Item {
	out := slices.Clone(items)

	slices.SortStableFunc(out, func(a, b Item) int {
		if a.IsPinned != b.IsPinned {
			if a.IsPinned {
				return -1
			}

			return 1
		}

		return b.CreatedAt.Compare(a.CreatedAt)
	})

	return out
}
