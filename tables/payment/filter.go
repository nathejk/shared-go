package payment

import "github.com/nathejk/shared-go/types"

// Filter narrows a GetAll. A zero Filter matches every payment.
//
// Both fields are what the callers across the repos actually pass: hq's admin
// list filters by year, its team page by team. The paging and sort-safelist
// machinery that used to live here was removed — no caller ever set Page,
// PageSize or Sort, GetAll never read them, and Validate/SortColumn/
// SortDirection/Offset/Limit had no callers at all. SortColumn also panicked on
// an unrecognised value; if sorting is reintroduced, have it return an error.
//
// It also pulled in a local validator package, which alone would have blocked
// moving this entity into shared-go.
type Filter struct {
	// Year matches payment.year, the year the payment was requested in.
	Year types.YearSlug

	// TeamIDs matches payments owned by any of these teams, whether linked
	// directly or through an order. See teamOwned.
	TeamIDs []types.TeamID
}
