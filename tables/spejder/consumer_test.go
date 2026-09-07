package spejder

import (
	"strings"
	"testing"

	"github.com/jrgensen/cqrs"
	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/messages"
)

func project(t *testing.T, subject string, body any) *cqrstest.Writer {
	t.Helper()
	w := &cqrstest.Writer{}
	c := &consumer{w: w}
	m := cqrstest.NewMessage(cqrs.SubjectFromStr(subject))
	if err := m.SetBody(body); err != nil {
		t.Fatalf("set body: %v", err)
	}
	if err := c.HandleMessage(m); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	return w
}

// The roster half of a pre-race reassignment: teamId actually changes.
//
// It is worth asserting because until this branch existed, no event in the system
// moved a member between teams on the roster — spejder.*.updated inserts with
// INSERT IGNORE and its UPDATE deliberately omits teamId — so re-emitting an
// updated event with a different team was a silent no-op.
func TestReassignedUpdatesTheTeam(t *testing.T) {
	w := project(t, "NATHEJK:2026.spejder.m-1.reassigned", &messages.NathejkMemberReassigned{
		MemberID:   "m-1",
		FromTeamID: "team-from",
		ToTeamID:   "team-to",
	})
	got := w.Last()
	if !strings.Contains(got, `teamId="team-to"`) {
		t.Errorf("want the team updated, got:\n%s", got)
	}
	// Scoped by year *and* memberId: the primary key is (year, memberId) and member
	// ids are not year-scoped, so an unscoped UPDATE would rewrite the same member's
	// row in every season they took part in.
	if !strings.Contains(got, `year="2026"`) || !strings.Contains(got, `memberId="m-1"`) {
		t.Errorf("the update must be scoped by year and memberId, got:\n%s", got)
	}
}

// The reassignment must be subscribed to, or the branch above is dead code.
func TestReassignedIsSubscribed(t *testing.T) {
	c := &consumer{}
	for _, s := range c.Consumes() {
		if strings.HasSuffix(s.Subject(), ".reassigned") {
			return
		}
	}
	t.Error("the reassignment subject is not in Consumes()")
}

// teamId must have exactly one writer. The updated branch keeps a member's details
// current and must keep leaving their team alone, or the roster would follow
// whatever a stale signup form last said.
func TestUpdatedStillDoesNotTouchTheTeam(t *testing.T) {
	w := project(t, "NATHEJK:2026.spejder.m-1.updated", &messages.NathejkScoutUpdated{
		MemberID: "m-1",
		Name:     "Alma",
	})
	for _, s := range w.Statements {
		if strings.HasPrefix(s, "UPDATE") && strings.Contains(s, "teamId") {
			t.Errorf("spejder.updated must not write teamId, got:\n%s", s)
		}
	}
}

// A reassignment naming no destination is not a request to clear somebody's team.
func TestReassignedIgnoresAnEmptyDestination(t *testing.T) {
	w := project(t, "NATHEJK:2026.spejder.m-1.reassigned", &messages.NathejkMemberReassigned{
		MemberID: "m-1",
	})
	if len(w.Statements) != 0 {
		t.Errorf("want no statement, got %v", w.Statements)
	}
}
