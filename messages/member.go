package messages

import (
	"time"

	"github.com/nathejk/shared-go/types"
)

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

// nathejk:*.member.*.verified
//
// NathejkMemberVerified says that a member has looked at the guardian/emergency contact number
// held for them and acknowledged that it can be reached during the event (hej, PRD 005).
//
// Published by `hej` — the app the member uses — and by nothing else. It is the first member
// fact that originates with the member rather than with the register.
//
// # Why two phone numbers
//
// The acknowledgement is a claim about a *specific* number ("this number can be contacted during
// Nathejk"), so it is only meaningful alongside the number it was made about. And the member may
// acknowledge a number that is NOT the one on file: if they cannot recognise the registered
// number they are asked to supply the correct one and confirm that instead.
//
//	PhoneParentAcknowledged — the number the member says can be reached. Authoritative for contacting
//	                    a guardian during the event.
//	PhoneParentRegistered   — what the register held at that moment. Kept so two different questions
//	                    stay answerable:
//	                      * has the register changed since? (PhoneRegistered != current) → the
//	                        acknowledgement is stale and must be asked again
//	                      * did the member correct us? (PhoneParentAcknowledged != PhoneParentRegistered) →
//	                        the register is wrong and an organizer should fix it
//
// Collapsing them into one field makes "stale" and "corrected" indistinguishable, and they call
// for opposite responses: re-ask the member, versus update the register and leave the member alone.
type NathejkMemberVerified struct {
	MemberID types.MemberID `json:"memberId"`
	Year     types.YearSlug `json:"year"`

	// PhoneParentAcknowledged is normalized. Never empty: a verification that names no number cannot
	// be checked for staleness later, so it would be a permanent tick for a phone nobody agreed
	// to — which is the expensive kind of wrong in an emergency-contact flow.
	PhoneParentAcknowledged types.PhoneNumber `json:"phoneParentAcknowledged"`

	// PhoneParentRegistered is what the register held when the member acknowledged. Empty is
	// meaningful: it says the register had no number and the member supplied one.
	PhoneParentRegistered types.PhoneNumber `json:"phoneParentRegistered,omitempty"`

	// VerifiedAt is when the member acknowledged, in UTC.
	//
	// On the event rather than derived from delivery time, because delivery time changes on
	// every replay and this timestamp answers "how many members verified before arriving?".
	VerifiedAt time.Time `json:"verifiedAt"`
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
