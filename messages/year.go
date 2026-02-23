package messages

import "github.com/nathejk/shared-go/types"

type NathejkYearCreated struct {
	Slug types.YearSlug `json:"slug"`
}

type NathejkYearUpdated struct {
	Slug            types.YearSlug `json:"slug"`
	Headline        *string        `json:"headline,omitempty"`
	Description     *string        `json:"description,omitempty"`
	CityDeparture   *string        `json:"cityDeparture,omitempty"`
	CityDestination *string        `json:"cityDestination,omitempty"`
	DateStart       *types.Date    `json:"dateStart,omitempty"`
	DateEnd         *types.Date    `json:"dateEnd,omitempty"`
}

type NathejkYearDeleted struct {
	Slug types.YearSlug `json:"slug"`
}
