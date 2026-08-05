package patrulje

import (
	"context"
	"testing"

	"github.com/jrgensen/cqrs"
	"github.com/jrgensen/cqrs/cqrstest"
)

func newTestCommander() (*commander, *cqrstest.Publisher) {
	pub := &cqrstest.Publisher{}
	return &commander{p: pub}, pub
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
