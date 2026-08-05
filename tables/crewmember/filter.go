package crewmember

import (
	"github.com/nathejk/shared-go/types"
)

// CrewMember is the projection of a crew-member aggregate.
type CrewMember struct {
	UserID      types.UserID       `json:"userId" db:"userId"`
	YearSlug    types.YearSlug     `json:"yearSlug" db:"year"`
	Name        string             `json:"name" db:"name"`
	Phone       types.PhoneNumber  `json:"phone" db:"phone"`
	Email       types.EmailAddress `json:"email" db:"email"`
	MedlemNr    string             `json:"medlemnr" db:"medlemNr"`
	Group       string             `json:"group" db:"groupName"`
	Corps       types.CorpsSlug    `json:"corps" db:"corps"`
	Diet        string             `json:"diet" db:"diet"`
	Additionals string             `json:"additionals" db:"additionals"`
	SectionSlug types.Slug         `json:"sectionSlug" db:"sectionSlug"`
}

// Filter used when querying crew members.
type Filter struct {
	YearSlug    types.YearSlug
	SectionSlug types.Slug
	Unassigned  bool
}
