package messages

import "github.com/nathejk/shared-go/types"

// nathejk:signedup
type NathejkTeamSignedUp struct {
	TeamID types.TeamID `json:"teamId"`
	//	Type   types.Enum   `json:"type"`
	//	Slug    string            `json:"slug"`
	Name    string            `json:"name"`
	Phone   types.PhoneNumber `json:"phone"`
	Email   types.Email       `json:"email"`
	Pincode string            `json:"pincode"`
}

// nathejk:phonenumber.confirmed
type NathejkSignupPincodeUsed struct {
	TeamID  types.TeamID      `json:"teamId"`
	Phone   types.PhoneNumber `json:"phone"`
	Pincode string            `json:"pincode"`
}

// nathejk:sms.sent
type NathejkSmsSent struct {
	PingType types.PingType    `json:"pingType"`
	TeamID   types.TeamID      `json:"teamId,omitempty"`
	Phone    types.PhoneNumber `json:"phone"`
	Text     string            `json:"text"`
	Error    string            `json:"error,omitempty"`
}

// nathejk:team.updated
type NathejkTeamUpdated struct {
	TeamID            types.TeamID      `json:"teamId"`
	Type              types.Enum        `json:"type"`
	Name              string            `json:"name"`
	GroupName         string            `json:"groupName"`
	Korps             string            `json:"korps"`
	AdvspejdNumber    string            `json:"advspejdNumber,omitempty"`
	ContactName       string            `json:"contactName"`
	ContactAddress    string            `json:"contactAddress,omitempty"`
	ContactPostalCode string            `json:"contactPostalCode,omitempty"`
	ContactEmail      types.Email       `json:"contactEmail"`
	ContactPhone      types.PhoneNumber `json:"contactPhone"`
	ContactRole       string            `json:"contactRole"`
}

// nathejk:klan.updated
type NathejkKlanUpdated struct {
	TeamID    types.TeamID `json:"teamId"`
	Name      string       `json:"name"`
	GroupName string       `json:"groupName"`
	Korps     string       `json:"korps"`
}

type NathejkTeamStatusChanged struct {
	TeamID types.TeamID       `json:"teamId"`
	Status types.SignupStatus `json:"signupStatus"`
}
type NathejkPatruljeStatusChanged NathejkTeamStatusChanged
type NathejkKlanStatusChanged NathejkTeamStatusChanged

// nathejk:patrol.created
type NathejkPatrolCreated struct {
	TeamID         types.TeamID `json:"teamId"`
	Year           uint         `json:"year"`
	Name           string       `json:"name"`
	GroupName      string       `json:"groupName"`
	Corps          string       `json:"korps"`
	AdvspejdNumber string       `json:"advspejdNumber,omitempty"`
}

// nathejk:patrol.updated
type NathejkPatrolUpdated struct {
	TeamID         types.TeamID `json:"teamId"`
	Name           string       `json:"name"`
	GroupName      string       `json:"groupName"`
	Corps          string       `json:"korps"`
	AdvspejdNumber string       `json:"advspejdNumber,omitempty"`
}

// nathejk:patrol.deleted
type NathejkPatrolDeleted struct {
	TeamID     types.TeamID `json:"teamId"`
	DeletedUts string       `json:"deletedUts"`
}

type NathejkPatrolContactUpdated struct {
	TeamID     types.TeamID      `json:"teamId"`
	Name       string            `json:"name"`
	Address    string            `json:"address"`
	PostalCode string            `json:"postalCode"`
	Email      types.Email       `json:"email"`
	Phone      types.PhoneNumber `json:"phone"`
	Role       string            `json:"role"`
}

type NathejkPatrolNumberAssigned struct {
	TeamID     types.TeamID `json:"teamId"`
	TeamNumber string       `json:"teamNumber"`
}

type NathejkPaymentKeyAssigned struct {
	TeamID     types.TeamID `json:"teamId"`
	PaymentKey string       `json:"paymentKey"`
}

type NathejkSquadCreated struct {
	TeamID    types.TeamID `json:"teamId"`
	Year      uint         `json:"year"`
	Name      string       `json:"name"`
	GroupName string       `json:"groupName"`
	Corps     string       `json:"korps"`
}
