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

// The member lifecycle events below are published on
// NATHEJK.{year}.spejder.{memberId}.{event} and projected by the spejderstatus
// table, which owns each member's status and team membership and derives
// patrulje.activeMemberCount from it.
//
// # Who may write what
//
// A member is *self-carrying* up to and including types.MemberStatusWaiting:
// they have covered every metre on their own legs and a phone call has not
// changed that. Those transitions belong to the nødtelefon. From the car door
// onwards each step is an **acceptance by the receiving party** — the driver
// accepts the member aboard, the shelter accepts them on arrival, a guardian or
// their own team accepts them at the end — and each of those interfaces
// publishes its own event. Custody is always confirmed by the receiver, never
// claimed by the party letting go, which is what makes the chain trustworthy.
//
// # One event per act, not one "status changed"
//
// A single generic status-changed event would fit this model badly. Each
// transition is a distinct act by a distinct party — a request to leave, a
// decision to carry on, an acceptance into a car, an acceptance at the shelter,
// a handover — so each gets its own type carrying the party responsible. That is
// what makes the *acceptor* recordable, which a bare {memberId, status} payload
// cannot express, and it answers "who is holding this member right now?" for
// free, because the car's acceptance event names the car.
//
// Each body resolves to exactly one types.MemberStatus, via Status(). A
// projection therefore never needs to know which event it is looking at in order
// to write a row, and a new transition added later cannot invent a status the
// lifecycle does not define.
//
// # No sosId, deliberately
//
// None of these carry a case id, even though the nødtelefon always has one when
// it publishes them. The case is a fact about *why the operator was on the
// phone*, not about the member, and the car and shelter interfaces publish these
// same events knowing nothing about cases at all. Putting a sosId here would
// either make it a lie for them or force them to invent one.
//
// The case link is carried by a separate summarising event on the sos entity,
// published once per operation after the member events it describes (PRD 006
// §8). So sosId is a parameter of the *command*, never a field on the event.

// NathejkMemberEvent is the shape every lifecycle event shares: it concerns one
// member and resolves to one status.
//
// Projections depend on this interface rather than on the concrete types, so
// adding a transition means adding a type and a subject, not editing the write
// path.
type NathejkMemberEvent interface {
	// Status is the status the member is in after this event.
	Status() types.MemberStatus
}

// NathejkMemberActor is who performed the act, resolved by the HTTP layer and
// passed in by the caller rather than read from a request context — these events
// are published from several services.
//
// Today the value is empty in practice: authentication is perimeter-only, so the
// middleware puts an anonymous user with no id on every request (PRD 001 §6
// Auth). It is recorded anyway, so that identity arriving later needs no change
// in the domain.
type NathejkMemberActor struct {
	UserID types.UserID `json:"userId,omitempty"`
	Name   string       `json:"name,omitempty"`
}

// NathejkMemberWithdrawalRequested records that the member wants to leave the
// race and is waiting to be collected.
//
// The member is still self-carrying: they have accepted no help beyond a phone
// call, so this is not yet a withdrawal and their finish is intact. What it does
// mean is that their patrol may not continue until they are either collected or
// back on their feet — this state blocks the whole team, which is why it is the
// one worth an alarm when it lasts too long.
//
// It also starts the clock on the count that must reach zero before the
// organisers can go home. From here until somebody takes charge of the member,
// they are in our care.
type NathejkMemberWithdrawalRequested struct {
	MemberID types.MemberID     `json:"memberId"`
	TeamID   types.TeamID       `json:"teamId"`
	Actor    NathejkMemberActor `json:"actor"`
}

func (NathejkMemberWithdrawalRequested) Status() types.MemberStatus {
	return types.MemberStatusWaiting
}

// NathejkMemberWithdrawalCancelled records that the member decided to carry on
// under their own steam.
//
// Legitimate and expected, not a correction: plenty of members stop for a
// blister, a bad stretch or a cry and then walk the rest of it themselves. They
// go back to racing with their finish intact, because sitting by the trail costs
// them time, not the route.
//
// Valid only while the member is still waiting. Once a car has accepted them the
// lift cannot be uncrossed, so the publishing command is expected to dirty-check
// the current row and reject this otherwise: the race between an operator
// pressing resume and a driver accepting the member aboard is resolved in the
// driver's favour.
type NathejkMemberWithdrawalCancelled struct {
	MemberID types.MemberID     `json:"memberId"`
	TeamID   types.TeamID       `json:"teamId"`
	Actor    NathejkMemberActor `json:"actor"`
}

func (NathejkMemberWithdrawalCancelled) Status() types.MemberStatus {
	return types.MemberStatusRacing
}

// NathejkMemberStatusOverridden corrects a member's status by hand.
//
// This is the admission that something happened which the interface that owns it
// did not record — most often, before the car and shelter interfaces exist, a
// pickup or an arrival that only reached us by radio. It is deliberately a
// separate event from the transitions above rather than a parameterised setter,
// so that "how often are we correcting by hand?" stays answerable: a high count
// means the chain of custody is fiction, and that is worth knowing.
//
// types.MemberStatusFinished is never a valid target. Only a member who walked
// the route unaided has finished, and CanFinish() is true only for racing, so the
// finish can never be conferred by correction.
type NathejkMemberStatusOverridden struct {
	MemberID types.MemberID     `json:"memberId"`
	TeamID   types.TeamID       `json:"teamId"`
	To       types.MemberStatus `json:"to"`
	Actor    NathejkMemberActor `json:"actor"`
}

func (e NathejkMemberStatusOverridden) Status() types.MemberStatus { return e.To }

// NathejkMemberTeamMoved records that the member now belongs to a different
// team.
//
// This is what replaces the legacy patrulje.merged / patrulje.splited pair. Teams
// are not merged and split; a member is moved, and a team left with nobody racing
// is thereby discontinued. The old encoding pointed a teamId at a parentTeamId
// and had to be deleted again to undo itself, which is precisely the drift this
// avoids: membership is the only input, so the team fact follows and reverses on
// its own.
//
// FromTeamID is carried so the projection can recompute activeMemberCount for
// *both* teams without reading the previous row — the origin team loses a member
// and the destination gains one, and a replay must produce the same two counts
// whatever order it sees things in.
//
// The member's status does not change: a survivor moved into another patrol is
// still racing and still self-carrying, so they can still finish — with a team
// that is not the one they started with, which is why initialTeamId is never
// overwritten.
type NathejkMemberTeamMoved struct {
	MemberID   types.MemberID     `json:"memberId"`
	FromTeamID types.TeamID       `json:"fromTeamId"`
	ToTeamID   types.TeamID       `json:"toTeamId"`
	Actor      NathejkMemberActor `json:"actor"`
}

func (NathejkMemberTeamMoved) Status() types.MemberStatus { return types.MemberStatusRacing }

// NathejkMemberPickupAccepted records that a car has taken the member aboard.
//
// Published by the dispatch desk (PRD 009, task 118), and eventually by the
// driver's own app — the driver accepts the member, and until they have a screen
// the dispatcher records it on their behalf.
//
// This is the point of no return. It is the first outside help the member has
// taken, so there is no way back onto the route and no finish to be had: the
// endings available from here are reunited and released.
//
// SectionSlug names the dispatch unit holding the member — the question a
// dashboard actually needs answered while somebody is in transit. A unit and not
// a vehicle id, deliberately: the unit is who took them, and it survives a car
// being swapped mid-night.
type NathejkMemberPickupAccepted struct {
	MemberID     types.MemberID     `json:"memberId"`
	TeamID       types.TeamID       `json:"teamId"`
	SectionSlug  types.Slug         `json:"sectionSlug,omitempty"`
	DriverUserID types.UserID       `json:"driverUserId,omitempty"`
	Actor        NathejkMemberActor `json:"actor"`
}

func (NathejkMemberPickupAccepted) Status() types.MemberStatus { return types.MemberStatusTransit }

// NathejkMemberShelterAccepted records that HQ has received the member and is
// looking after them — put to bed if it is the middle of the night, waiting in
// the warm if somebody is already on the way. Which of the two depends on the
// hour rather than on anything worth tracking, so it is one state.
//
// Published by the shelter interface (PRD 007), which is also the only party that
// can say it: the receiver confirms custody, never the party letting go.
//
// Placement is where in the shelter the member was put, and it is optional
// because the two facts arrive together in the ordinary case and separately in
// the awkward one. A crew member receiving three scouts off a car types the tent
// once for all of them; a crew member receiving somebody at a run records the
// arrival now and where they ended up when they get back. Requiring it would push
// the second case into either a lie or a second screen.
type NathejkMemberShelterAccepted struct {
	MemberID  types.MemberID     `json:"memberId"`
	TeamID    types.TeamID       `json:"teamId"`
	Placement string             `json:"placement,omitempty"`
	Actor     NathejkMemberActor `json:"actor"`
}

func (NathejkMemberShelterAccepted) Status() types.MemberStatus {
	return types.MemberStatusSheltered
}

// NathejkMemberShelterPlaced records where in the shelter the member is — the
// answer to "which tent is she in?", asked at 3am by a parent standing at the
// door.
//
// Its own event rather than a re-published NathejkMemberShelterAccepted, because
// moving a sleeping child from one tent to another is a distinct act and reads as
// one on the timeline. A re-publish would also claim custody was taken twice,
// which is exactly the fiction the acceptance events exist to prevent.
//
// The status it resolves to is `sheltered`, unchanged: placing somebody does not
// move them through the lifecycle. The projection's write is therefore idempotent
// and the placement is the point — which is why the placering lives in hq's own
// `shelter` table rather than on spejderstatus. A bed is a fact about the
// shelter, not about the lifecycle.
//
// Placement is deliberately free text. The zones are not known until race start
// (PRD 007 §6), so no vocabulary can be defined here; the interface suggests what
// is already in use and enforces nothing.
type NathejkMemberShelterPlaced struct {
	MemberID  types.MemberID     `json:"memberId"`
	TeamID    types.TeamID       `json:"teamId"`
	Placement string             `json:"placement"`
	Actor     NathejkMemberActor `json:"actor"`
}

func (NathejkMemberShelterPlaced) Status() types.MemberStatus { return types.MemberStatusSheltered }

// NathejkMemberHandoverCompleted records that somebody else has taken charge of
// the member and we no longer track them. This is what takes them out of the
// in-our-care count.
//
// Two endings, and they are not interchangeable, which is why To is on the event
// rather than being guessed from the hour: released means a guardian came for
// them during the night, reunited means their own team reached the finish and the
// member was handed back to it. Neither is finished — see
// types.MemberStatusFinished for why keeping that apart is what lets a finish be
// counted as an achievement rather than as attendance.
type NathejkMemberHandoverCompleted struct {
	MemberID types.MemberID     `json:"memberId"`
	TeamID   types.TeamID       `json:"teamId"`
	To       types.MemberStatus `json:"to"`
	Actor    NathejkMemberActor `json:"actor"`
}

func (e NathejkMemberHandoverCompleted) Status() types.MemberStatus { return e.To }
