package signup

import (
	"context"
	"testing"

	"github.com/jrgensen/cqrs"
	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/types"
)

// fakeQueries answers GetByID with one signup, so a command's subject can be
// asserted without a database.
type fakeQueries struct{ signup *Signup }

func (f fakeQueries) GetByID(context.Context, types.TeamID) (*Signup, error) {
	return f.signup, nil
}

func newTestCommander(s *Signup) (*commander, *cqrstest.Publisher) {
	pub := &cqrstest.Publisher{}
	return &commander{p: pub, q: fakeQueries{signup: s}}, pub
}

// The verified subjects had their Sprintf arguments shifted by one, so every
// event went out as NATHEJK.<teamType>.klan.<id>.… — no year, and a patrulje
// labelled klan. Nothing user-facing broke, because the consumer matches on
// wildcards and updates from the body, which is exactly why it went unnoticed;
// what was lost was every use of the subject for filtering, routing or
// monitoring. These tests pin all three slots.
func TestVerifiedSubjectsCarryYearAndTeamType(t *testing.T) {
	for _, tc := range []struct {
		name     string
		teamType types.TeamType
		want     string
	}{
		{"patrulje", types.TeamTypePatrulje, "NATHEJK.2026.patrulje.team-1"},
		{"klan", types.TeamTypeKlan, "NATHEJK.2026.klan.team-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Signup{
				TeamID:   "team-1",
				Year:     "2026",
				TeamType: tc.teamType,
				Pincode:  "2574",
			}

			c, pub := newTestCommander(s)
			if err := c.VerifyPhone(context.Background(), "team-1", "2574"); err != nil {
				t.Fatalf("VerifyPhone: %v", err)
			}
			if got, want := pub.Subjects()[0], tc.want+".phonenumber.verified"; got != want {
				t.Errorf("phone subject = %q, want %q", got, want)
			}

			c, pub = newTestCommander(s)
			if err := c.VerifyEmail(context.Background(), "team-1", "a-secret"); err != nil {
				t.Fatalf("VerifyEmail: %v", err)
			}
			if got, want := pub.Subjects()[0], tc.want+".emailaddress.verified"; got != want {
				t.Errorf("email subject = %q, want %q", got, want)
			}
		})
	}
}

// The consumer must keep matching the malformed subjects already in the stream:
// they have the same arity, so a replay still projects them, and narrowing the
// patterns to the correct shape would orphan every historical event.
func TestConsumerPatternsMatchBothOldAndNewSubjects(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		subject string
	}{
		{"old phone", "NATHEJK.*.*.*.phonenumber.verified", "NATHEJK.patrulje.klan.team-1.phonenumber.verified"},
		{"new phone", "NATHEJK.*.*.*.phonenumber.verified", "NATHEJK.2026.patrulje.team-1.phonenumber.verified"},
		{"old email", "NATHEJK.*.*.*.emailaddress.verified", "NATHEJK.patrulje.klan.team-1.emailaddress.verified"},
		{"new email", "NATHEJK.*.*.*.emailaddress.verified", "NATHEJK.2026.patrulje.team-1.emailaddress.verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !cqrs.SubjectFromStr(tc.subject).Match(tc.pattern) {
				t.Errorf("%q must match %q, or a replay orphans it", tc.subject, tc.pattern)
			}
		})
	}
}
