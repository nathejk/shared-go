package messages

import "github.com/nathejk/shared-go/types"

type NathejkCrewMemberRegistered struct {
	UserID types.UserID       `json:"userId"`
	Name   string             `json:"name"`
	Phone  types.PhoneNumber  `json:"phone"`
	Email  types.EmailAddress `json:"email"`
}

type NathejkCrewMemberDeleted struct {
	UserID types.UserID `json:"userId"`
}

type NathejkCrewMemberUpdated struct {
	UserID      types.UserID       `json:"userId"`
	Name        string             `json:"name,omitempty"`
	Phone       types.PhoneNumber  `json:"phone,omitempty"`
	Email       types.EmailAddress `json:"email,omitempty"`
	MedlemNr    string             `json:"medlemnr,omitempty"`
	Group       string             `json:"group,omitempty"`
	Corps       types.CorpsSlug    `json:"corps,omitempty"`
	Diet        string             `json:"diet,omitempty"`
	Additionals map[string]any     `json:"additionals,omitempty"`
}

type NathejkCrewMemberSectionAssigned struct {
	UserID      types.UserID `json:"userId"`
	SectionSlug types.Slug   `json:"sectionSlug"`
}

type NathejkSectionAdded struct {
	Slug              types.Slug `json:"slug"`
	ParentSectionSlug types.Slug `json:"sectionSlug,omitempty"`
	Label             string     `json:"label,omitempty"`
	Type              types.Slug `json:"type,omitempty"`
	SelfAssignable    *bool      `json:"selfAssignable,omitempty"`
}

type NathejkSectionSorted struct {
	Slugs []types.Slug `json:"slugs"`
}

type NathejkSectionDeleted struct {
	Slug types.Slug `json:"slug"`
}
