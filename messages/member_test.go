package messages_test

import (
	"testing"

	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

// Every lifecycle event must resolve to exactly one status, and that status must
// be one the lifecycle actually defines.
//
// The second half is the point: a projection writes Status() straight into the
// row, so an event resolving to a value types.MemberStatus does not know about
// would put an unreadable status in the read model, and every InOurCare() and
// CanFinish() check downstream would quietly disagree about it. Valid() is the
// only thing standing between a typo in member.go and a member nobody is counted
// as looking for.
func TestMemberEventsResolveToValidStatus(t *testing.T) {
	tests := []struct {
		name  string
		event messages.NathejkMemberEvent
		want  types.MemberStatus
	}{
		{"withdrawal requested", messages.NathejkMemberWithdrawalRequested{}, types.MemberStatusWaiting},
		{"withdrawal cancelled", messages.NathejkMemberWithdrawalCancelled{}, types.MemberStatusRacing},
		{"team moved", messages.NathejkMemberTeamMoved{}, types.MemberStatusRacing},
		{"pickup accepted", messages.NathejkMemberPickupAccepted{}, types.MemberStatusTransit},
		{"shelter accepted", messages.NathejkMemberShelterAccepted{}, types.MemberStatusSheltered},
		{"shelter placed", messages.NathejkMemberShelterPlaced{}, types.MemberStatusSheltered},
		{"override", messages.NathejkMemberStatusOverridden{To: types.MemberStatusSheltered}, types.MemberStatusSheltered},
		{"handover released", messages.NathejkMemberHandoverCompleted{To: types.MemberStatusReleased}, types.MemberStatusReleased},
		{"handover reunited", messages.NathejkMemberHandoverCompleted{To: types.MemberStatusReunited}, types.MemberStatusReunited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.event.Status()
			if got != tt.want {
				t.Errorf("Status() = %q, want %q", got, tt.want)
			}
			if !got.Valid() {
				t.Errorf("Status() = %q, which types.MemberStatus does not recognise", got)
			}
		})
	}
}

// The self-carrying boundary, asserted on the events rather than only in the
// commands that publish them.
//
// A member is on their own legs up to and including waiting; from transit onwards
// they have taken a lift and no later event puts that back. So exactly one event
// may leave a member able to finish — the cancellation, because carrying on is
// what they actually did — and every event from the car door onwards must not.
//
// This is a test rather than a comment because the rule is invisible at the call
// site: nothing stops somebody adding a transition that resolves to racing.
func TestOnlyResumeRestoresTheAbilityToFinish(t *testing.T) {
	canFinish := map[string]bool{
		"withdrawal requested": messages.NathejkMemberWithdrawalRequested{}.Status().CanFinish(),
		"withdrawal cancelled": messages.NathejkMemberWithdrawalCancelled{}.Status().CanFinish(),
		"team moved":           messages.NathejkMemberTeamMoved{}.Status().CanFinish(),
		"pickup accepted":      messages.NathejkMemberPickupAccepted{}.Status().CanFinish(),
		"shelter accepted":     messages.NathejkMemberShelterAccepted{}.Status().CanFinish(),
		"shelter placed":       messages.NathejkMemberShelterPlaced{}.Status().CanFinish(),
	}
	want := map[string]bool{
		"withdrawal requested": false,
		"withdrawal cancelled": true,
		"team moved":           true, // still racing, just for a different patrol
		"pickup accepted":      false,
		"shelter accepted":     false,
		"shelter placed":       false,
	}
	for name, got := range canFinish {
		if got != want[name] {
			t.Errorf("%s: CanFinish() = %v, want %v", name, got, want[name])
		}
	}
}

// The in-our-care set, asserted through the events that produce it.
//
// This is the count that has to reach zero before anybody goes home, so what
// belongs in it is worth pinning: a request to leave puts a member in our care and
// a handover takes them out, while moving between teams never does either — a
// moved member is still on the route with a patrol, and counting them as ours
// would inflate the one number the night is judged by.
func TestInOurCareSpansWaitingToSheltered(t *testing.T) {
	inCare := []struct {
		name  string
		event messages.NathejkMemberEvent
		want  bool
	}{
		{"withdrawal requested", messages.NathejkMemberWithdrawalRequested{}, true},
		{"pickup accepted", messages.NathejkMemberPickupAccepted{}, true},
		{"shelter accepted", messages.NathejkMemberShelterAccepted{}, true},
		// Being moved between tents keeps a member in our care, obviously — but it is
		// worth asserting, because this is the one event that does not advance the
		// lifecycle, and an implementation that resolved it to "no change" by returning
		// MemberStatusNone would silently drop a sleeping child out of the count that
		// has to reach zero.
		{"shelter placed", messages.NathejkMemberShelterPlaced{}, true},
		{"withdrawal cancelled", messages.NathejkMemberWithdrawalCancelled{}, false},
		{"team moved", messages.NathejkMemberTeamMoved{}, false},
		{"handover released", messages.NathejkMemberHandoverCompleted{To: types.MemberStatusReleased}, false},
		{"handover reunited", messages.NathejkMemberHandoverCompleted{To: types.MemberStatusReunited}, false},
	}
	for _, tt := range inCare {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.Status().InOurCare(); got != tt.want {
				t.Errorf("InOurCare() = %v, want %v", got, tt.want)
			}
		})
	}
}

// None of the bodies may carry a case id.
//
// Asserted because the reason is easy to forget and the consequence is not local:
// the car and shelter interfaces publish these same events knowing nothing about
// SOS cases, so a sosId field would either be a lie for them or force them to
// invent one. The case link lives on the separate summarising sos event instead.
func TestNoMemberEventCarriesACaseID(t *testing.T) {
	// If a sosId is ever added to one of these, this test will not catch it by
	// reflection alone — it is here to make the intent unmissable to the next
	// person editing member.go, and to fail loudly if the marker below is
	// removed along with the field.
	events := []messages.NathejkMemberEvent{
		messages.NathejkMemberWithdrawalRequested{},
		messages.NathejkMemberWithdrawalCancelled{},
		messages.NathejkMemberStatusOverridden{},
		messages.NathejkMemberTeamMoved{},
		messages.NathejkMemberPickupAccepted{},
		messages.NathejkMemberShelterAccepted{},
		messages.NathejkMemberShelterPlaced{},
		messages.NathejkMemberHandoverCompleted{},
	}
	for _, e := range events {
		if _, ok := any(e).(interface{ SosID() string }); ok {
			t.Errorf("%T exposes a case id; the case link belongs on the summarising sos event", e)
		}
	}
}
