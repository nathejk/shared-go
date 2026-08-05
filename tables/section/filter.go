package section

import (
	"github.com/nathejk/shared-go/types"
)

// Section is the projection of a section aggregate.
type Section struct {
	Slug       types.Slug     `json:"slug" db:"slug"`
	YearSlug   types.YearSlug `json:"yearSlug" db:"year"`
	ParentSlug types.Slug     `json:"parentSlug,omitempty" db:"parentSlug"`
	Label      string         `json:"label" db:"label"`
	SortOrder  int            `json:"sortOrder" db:"sortOrder"`
}

// Filter used when querying sections.
type Filter struct {
	YearSlug types.YearSlug
	Slug     types.Slug
}
