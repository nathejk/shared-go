package patrulje

import (
	"context"
	"testing"

	"github.com/jrgensen/cqrs"
	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// fakeQueries answers with one patrulje, so the commands can be driven without
// a database. Its Year is what every published subject must carry — the season
// the team signed up for.
type fakeQueries struct {
	patrulje *Patrulje
	last     *Patrulje
}

func (f fakeQueries) GetByID(context.Context, types.TeamID) (*Patrulje, error) {
	if f.patrulje == nil {
		return nil, tables.ErrRecordNotFound
	}
	return f.patrulje, nil
}

func (f fakeQueries) GetLastWithNumber(context.Context) (*Patrulje, error) {
	if f.last == nil {
		return nil, tables.ErrRecordNotFound
	}
	return f.last, nil
}

func newTestCommander() (*commander, *cqrstest.Publisher) {
	pub := &cqrstest.Publisher{}
	q := fakeQueries{patrulje: &Patrulje{TeamID: "team-1", Year: "2026"}}
	return &commander{p: pub, q: q}, pub
}

// drain returns everything published since the last call, and clears the spy.
func drain(pub *cqrstest.Publisher) []cqrs.Message {
	msgs := pub.Messages
	pub.Reset()
	return msgs
}

func TestAddMemberPublishesOneCreateEvent(t *testing.T) {
	c, pub := newTestCommander()
	id, err := c.AddMember(context.Background(), "team-1", Spejder{Name: "Anna"})
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if id == "" {
		t.Fatal("AddMember should issue a memberId")
	}
	msgs := drain(pub)
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 event, got %d", len(msgs))
	}
	if !msgs[0].Subject().Match("nathejk.*.spejder.*.updated") {
		t.Errorf("unexpected subject %q", msgs[0].Subject().Subject())
	}
	// The create path carries teamId so the projector inserts the row.
	var body struct {
		TeamID string `json:"teamId"`
	}
	if err := msgs[0].Body(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.TeamID == "" {
		t.Error("add event should carry teamId (create path)")
	}
}

func TestUpdateMemberPublishesOneUpdateEventWithoutCreate(t *testing.T) {
	c, pub := newTestCommander()
	if err := c.UpdateMember(context.Background(), "team-1", Spejder{MemberID: "m-1", Name: "Anna", TShirtSize: "l"}); err != nil {
		t.Fatalf("UpdateMember: %v", err)
	}
	msgs := drain(pub)
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 event, got %d", len(msgs))
	}
	if !msgs[0].Subject().Match("nathejk.*.spejder.*.updated") {
		t.Errorf("unexpected subject %q", msgs[0].Subject().Subject())
	}
	// Update must NOT carry teamId, so the projector performs a pure UPDATE and
	// never resurrects/creates a member.
	var body struct {
		TeamID string `json:"teamId"`
	}
	if err := msgs[0].Body(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.TeamID != "" {
		t.Errorf("update event must not carry teamId, got %q", body.TeamID)
	}
}

func TestUpdateMemberRejectsEmptyID(t *testing.T) {
	c, _ := newTestCommander()
	if err := c.UpdateMember(context.Background(), "team-1", Spejder{}); err == nil {
		t.Fatal("UpdateMember with empty memberId should error")
	}
}

func TestDeleteMemberPublishesOneDeleteEvent(t *testing.T) {
	c, pub := newTestCommander()
	if err := c.DeleteMember(context.Background(), "team-1", "m-1"); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	msgs := drain(pub)
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 event, got %d", len(msgs))
	}
	if !msgs[0].Subject().Match("nathejk.*.spejder.*.deleted") {
		t.Errorf("unexpected subject %q", msgs[0].Subject().Subject())
	}
}

// TestUpdateEmitsTeamEventOnly guards that a routine team save publishes only
// the team-updated event and never a member event (no churn from the roster PUT).
func TestUpdateEmitsTeamEventOnly(t *testing.T) {
	c, pub := newTestCommander()
	if err := c.Update(context.Background(), "team-1", Team{Name: "Team"}, Contact{Name: "Contact"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	msgs := drain(pub)
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 event, got %d", len(msgs))
	}
	if !msgs[0].Subject().Match("nathejk.*.patrulje.*.updated") {
		t.Errorf("unexpected subject %q", msgs[0].Subject().Subject())
	}
}

// The season comes off the team's row, which inherited it from the signup — via
// the signedup subject, not the projector's wall clock. These subjects used to
// carry a literal "2026".
func TestSubjectsUseTheTeamsSeason(t *testing.T) {
	pub := &cqrstest.Publisher{}
	q := fakeQueries{patrulje: &Patrulje{TeamID: "team-1", Year: "2027"}}
	c := &commander{p: pub, q: q}

	if _, err := c.AddMember(context.Background(), "team-1", Spejder{Name: "Bo"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := c.AssignNumber(context.Background(), "team-1"); err != nil {
		t.Fatalf("AssignNumber: %v", err)
	}
	for _, subj := range pub.Subjects() {
		if !cqrs.SubjectFromStr(subj).Match("NATHEJK.2027.*.*.*") {
			t.Errorf("subject %q should carry the team's season", subj)
		}
	}
}

func TestCommandsFailWhenTeamIsUnknown(t *testing.T) {
	pub := &cqrstest.Publisher{}
	c := &commander{p: pub, q: fakeQueries{patrulje: nil}}

	if _, err := c.AddMember(context.Background(), "team-1", Spejder{Name: "Bo"}); err == nil {
		t.Error("AddMember should fail when the team is unknown")
	}
	if err := c.AssignNumber(context.Background(), "team-1"); err == nil {
		t.Error("AssignNumber should fail when the team is unknown")
	}
	if len(pub.Messages) != 0 {
		t.Errorf("nothing should be published, got %v", pub.Subjects())
	}
}
