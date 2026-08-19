package order

import (
	"fmt"
	"sort"
	"strings"
)

// VariantKey identifies a purchasable variant: a product plus the size it was
// bought in. Size is "" for SKUs that have no sizes (every participation.*),
// which makes the sizeless case a variant like any other rather than a branch.
type VariantKey struct {
	SKU  string
	Size string
}

// sizeAttribute is the attribute key carrying a line's size. One constant, so
// the projector, the paid-quantity query and the offset cannot disagree about
// where a size lives.
const sizeAttribute = "size"

// lineSize extracts a line's normalised size from its attributes.
//
// Normalisation is not cosmetic. Variant matching compares these strings, so a
// producer writing "XXL" where another writes "xxl" would look like two
// different sizes: the offset would credit one and charge the other, silently
// inventing a size change nobody made. That is the same class of invisible
// wrongness this whole mechanism exists to remove, so the comparison is made on
// a canonical form.
//
// Values are stringified rather than type-asserted: attributes arrive from JSON,
// where a size could plausibly show up as a number ("2"), and a mismatch in Go
// type should not read as a mismatch in size.
func lineSize(attrs map[string]any) string {
	v, ok := attrs[sizeAttribute]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return normalizeSize(s)
	}
	return normalizeSize(fmt.Sprint(v))
}

func normalizeSize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// orderedSizes returns the paid sizes present in paid, in the order they should
// be reclaimed when a size change has to pick one.
//
// Catalogue order first, because that is the sequence a human reading the lines
// expects: crediting the xl before the 3xl is meaningful, whereas sorting slugs
// would credit "3xl" first purely because "3" < "x". Sizes no longer in the
// catalogue (a slug retired since it was bought) come last, ordered by slug, so
// that the ordering is total and therefore deterministic — which the sync check
// depends on: two runs that pick different-but-equivalent sizes would republish
// the lines on every read.
func orderedSizes(paid map[string]int, catalogue []string) []string {
	rank := make(map[string]int, len(catalogue))
	for i, s := range catalogue {
		rank[normalizeSize(s)] = i
	}

	sizes := make([]string, 0, len(paid))
	for s := range paid {
		sizes = append(sizes, s)
	}
	sort.Slice(sizes, func(i, j int) bool {
		ri, iOK := rank[sizes[i]]
		rj, jOK := rank[sizes[j]]
		switch {
		case iOK && jOK:
			return ri < rj
		case iOK != jOK:
			return iOK // catalogue sizes before unknown ones
		default:
			return sizes[i] < sizes[j]
		}
	})
	return sizes
}
