package patrulje

import (
	"strings"

	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

type Filter struct {
	YearSlug     types.YearSlug
	Page         int
	PageSize     int
	Sort         string
	SortSafelist []string
	TeamID       types.TeamID
}

func (f *Filter) Validate(v tables.Validator) {
	// Check that the page and page_size parameters contain sensible values.
	v.Check(f.Page > 0, "page", "must be greater than zero")
	v.Check(f.PageSize > 0, "page_size", "must be greater than zero")

	// Check that the sort parameter matches a value in the safelist.
	v.Check(tables.PermittedValue(f.Sort, f.SortSafelist...), "sort", "invalid sort value")
}

// Check that the client-provided Sort field matches one of the entries in our safelist
// and if it does, extract the column name from the Sort field by stripping the leading
// hyphen character (if one exists).
func (f Filter) SortColumn() string {
	for _, safeValue := range f.SortSafelist {
		if f.Sort == safeValue {
			return strings.TrimPrefix(f.Sort, "-")
		}
	}
	panic("unsafe sort parameter: " + f.Sort)
}

// Return the sort direction ("ASC" or "DESC") depending on the prefix character of the
// Sort field.
func (f Filter) SortDirection() string {
	if strings.HasPrefix(f.Sort, "-") {
		return "DESC"
	}
	return "ASC"
}

func (f Filter) Offset() int {
	return (f.Page - 1) * f.PageSize
}

func (f Filter) Limit() int {
	return f.PageSize
}

type Metadata struct {
	Year         string       `json:"year"`
	TeamID       types.TeamID `json:"teamId,omitempty"`
	CurrentPage  int          `json:"current_page,omitempty"`
	PageSize     int          `json:"page_size,omitempty"`
	FirstPage    int          `json:"first_page,omitempty"`
	LastPage     int          `json:"last_page,omitempty"`
	TotalRecords int          `json:"total_records,omitempty"`
}
