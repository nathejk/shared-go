package klan

import (
	"context"
	"testing"

	"github.com/jrgensen/cqrs"
	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// fakeQueries answers with one klan, so the commands can be driven without a
// database. Its Year is what every published subject must carry — the season the
// team signed up for, which the projector took from the signup's subject.
type fakeQueries struct {
	klan        *Klan
	seniorCount int
}

func (f fakeQueries) GetByID(context.Context, types.TeamID) (*Klan, error) {
	if f.klan == nil {
		return nil, tables.ErrRecordNotFound
	}
	return f.klan, nil
}
func (f fakeQueries) RequestedSeniorCount(context.Context, types.YearSlug) (int, error) {
	return f.seniorCount, nil
}
func (fakeQueries) RequestedMemberCount(context.Context, types.YearSlug) (uint32, error) {
	return 0, nil
}
func (fakeQueries) GetAll(context.Context, Filter) ([]Klan, error) { return nil, nil }

func newTestCommander() (*commander, *cqrstest.Publisher) {
	pub := &cqrstest.Publisher{}
	q := fakeQueries{klan: &Klan{ID: "team-1", Year: "2026"}}
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
	id, err := c.AddMember(context.Background(), "team-1", Senior{Name: "Bo"})
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
	if !msgs[0].Subject().Match("nathejk.*.senior.*.updated") {
		t.Errorf("unexpected subject %q", msgs[0].Subject().Subject())
	}
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
	if err := c.UpdateMember(context.Background(), "team-1", Senior{MemberID: "m-1", Name: "Bo"}); err != nil {
		t.Fatalf("UpdateMember: %v", err)
	}
	msgs := drain(pub)
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 event, got %d", len(msgs))
	}
	if !msgs[0].Subject().Match("nathejk.*.senior.*.updated") {
		t.Errorf("unexpected subject %q", msgs[0].Subject().Subject())
	}
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

func TestDeleteMemberPublishesOneDeleteEvent(t *testing.T) {
	c, pub := newTestCommander()
	if err := c.DeleteMember(context.Background(), "team-1", "m-1"); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	msgs := drain(pub)
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 event, got %d", len(msgs))
	}
	if !msgs[0].Subject().Match("nathejk.*.senior.*.deleted") {
		t.Errorf("unexpected subject %q", msgs[0].Subject().Subject())
	}
}

// The season comes off the team's row, which inherited it from the signup, so a
// team from another year publishes into that year. These subjects used to carry
// a literal "2026", which would have been silently wrong from 1 January 2027.
func TestMemberSubjectsUseTheTeamsSeason(t *testing.T) {
	pub := &cqrstest.Publisher{}
	c := &commander{p: pub, q: fakeQueries{klan: &Klan{ID: "team-1", Year: "2027"}}}

	if _, err := c.AddMember(context.Background(), "team-1", Senior{Name: "Bo"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := c.UpdateMember(context.Background(), "team-1", Senior{MemberID: "m-1"}); err != nil {
		t.Fatalf("UpdateMember: %v", err)
	}
	if err := c.DeleteMember(context.Background(), "team-1", "m-1"); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	for _, subj := range pub.Subjects() {
		if !cqrs.SubjectFromStr(subj).Match("NATHEJK.2027.senior.*.*") {
			t.Errorf("subject %q should carry the team's season", subj)
		}
	}
}

// Without a team row there is no season to name, and a subject with an empty
// year slot is worse than no event at all.
func TestMemberCommandsFailWhenTeamIsUnknown(t *testing.T) {
	pub := &cqrstest.Publisher{}
	c := &commander{p: pub, q: fakeQueries{klan: nil}}

	if _, err := c.AddMember(context.Background(), "team-1", Senior{Name: "Bo"}); err == nil {
		t.Error("AddMember should fail when the team is unknown")
	}
	if err := c.UpdateMember(context.Background(), "team-1", Senior{MemberID: "m-1"}); err == nil {
		t.Error("UpdateMember should fail when the team is unknown")
	}
	if err := c.DeleteMember(context.Background(), "team-1", "m-1"); err == nil {
		t.Error("DeleteMember should fail when the team is unknown")
	}
	if len(pub.Messages) != 0 {
		t.Errorf("nothing should be published, got %v", pub.Subjects())
	}
}
