package messages

import "github.com/nathejk/shared-go/types"

// nathejk:user.updated
type NathejkUserUpdated struct {
	UserID   types.UserID      `json:"userId"`
	Name     string            `json:"name"`
	Phone    types.PhoneNumber `json:"phone"`
	HqAccess bool              `json:"hqAccess"`
	Group    string            `json:"group"`
}

// nathejk:user.deleted
type NathejkUserDeleted struct {
	UserID types.UserID `json:"userId"`
}

// nathejk:personnel.updated
type NathejkPersonnelUpdated struct {
	UserID      types.UserID       `json:"userId"`
	Name        string             `json:"name"`
	Phone       types.PhoneNumber  `json:"phone"`
	Email       types.EmailAddress `json:"email"`
	HqAccess    bool               `json:"hqAccess"`
	Pincode     string             `json:"pincode"`
	Department  string             `json:"department"`
	MedlemNr    string             `json:"medlemnr"`
	Klan        string             `json:"klan"`
	Group       string             `json:"group"`
	Corps       types.CorpsSlug    `json:"corps"`
	TshirtSize  string             `json:"tshirtsize"`
	Diet        string             `json:"diet"`
	Additionals map[string]any     `json:"additionals"`
}

type NathejkPersonnelCreated struct {
	UserID  types.UserID      `json:"userId"`
	Phone   types.PhoneNumber `json:"phone"`
	Pincode string            `json:"pincode"`
}

// nathejk:personnel.deleted
type NathejkPersonnelDeleted struct {
	UserID types.UserID `json:"userId"`
}
