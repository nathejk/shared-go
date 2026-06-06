package messages

import "github.com/nathejk/shared-go/types"

// nathejk:member.updated
type NathejkMemberUpdated struct {
	MemberID    types.MemberID     `json:"memberId"`
	TeamID      types.TeamID       `json:"teamId"`
	Name        string             `json:"name"`
	Address     string             `json:"address,omitempty"`
	PostalCode  string             `json:"postalCode,omitempty"`
	City        string             `json:"city,omitempty"`
	Email       types.EmailAddress `json:"mail,omitempty"`
	Phone       types.PhoneNumber  `json:"phone,omitempty"`
	PhoneParent types.PhoneNumber  `json:"phoneParent,omitempty"`
	Birthday    types.Date         `json:"birthday,omitempty"`
	Returning   bool               `json:"returning"`
}

// nathejk:member.deleted
type NathejkMemberDeleted struct {
	MemberID types.MemberID `json:"memberId"`
	TeamID   types.TeamID   `json:"teamId"`
}
type NathejkMemberAdded struct {
	MemberID types.MemberID `json:"memberId"`
	TeamID   types.TeamID   `json:"teamId"`
}

type NathejkScoutUpdated struct {
	MemberID     types.MemberID     `json:"memberId"`
	Name         string             `json:"name,omitempty"`
	Address      string             `json:"address,omitempty"`
	PostalCode   string             `json:"postalCode,omitempty"`
	City         string             `json:"city,omitempty"`
	Email        types.EmailAddress `json:"mail,omitempty"`
	Phone        types.PhoneNumber  `json:"phone,omitempty"`
	PhoneContact types.PhoneNumber  `json:"phoneContact,omitempty"`
	BirthDate    types.Date         `json:"birthDate,omitempty"`
	Returning    bool               `json:"returning"`
	TShirtSize   string             `json:"tshirtsize,omitempty"`
}
type NathejkSeniorUpdated struct {
	MemberID   types.MemberID     `json:"memberId"`
	Name       string             `json:"name"`
	Address    string             `json:"address"`
	PostalCode string             `json:"postalCode"`
	City       string             `json:"city"`
	Email      types.EmailAddress `json:"mail"`
	Phone      types.PhoneNumber  `json:"phone"`
	BirthDate  types.Date         `json:"birthDate"`
	Returning  bool               `json:"returning"`
	TShirtSize string             `json:"tshirtsize"`
	Diet       string             `json:"diet"`
}

type NathejkScoutDeleted struct {
	MemberID   types.MemberID `json:"memberId"`
	TeamID     types.TeamID   `json:"teamId"`
	DeletedUts string         `json:"deletedUts,omitempty"`
}
