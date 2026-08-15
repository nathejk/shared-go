package vehicle

import (
	"context"
	"strings"
	"testing"

	"github.com/jrgensen/cqrs"
	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

func newTestCommander() (commander, *cqrstest.Publisher) {
	pub := &cqrstest.Publisher{}
	return commander{p: pub}, pub
}

// fakeQueries answers GetByID with vehicle, or not-found when it is nil, so the
// no-op checks in the assign commands can be exercised without a database.
type fakeQueries struct{ vehicle *Vehicle }

func (f fakeQueries) GetByID(context.Context, types.VehicleID) (*Vehicle, error) {
	if f.vehicle == nil {
		return nil, tables.ErrRecordNotFound
	}
	return f.vehicle, nil
}
func (fakeQueries) GetAll(context.Context, Filter) ([]Vehicle, error) { return nil, nil }

func registerFields() RegisterFields {
	return RegisterFields{
		LicensePlate:    "DK+AB12345",
		CustodianUserID: "user-1",
		Color:           "red",
		Brand:           "VW",
		Model:           "Transporter",
		SeatCount:       8,
	}
}

func TestRegisterPublishesOneEventWithAnID(t *testing.T) {
	c, pub := newTestCommander()

	id, err := c.Register(context.Background(), "2026", registerFields())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !strings.HasPrefix(string(id), "vehicle-") {
		t.Errorf("Register should mint a vehicle id, got %q", id)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.Messages))
	}
	if !pub.Messages[0].Subject().Match("NATHEJK.2026.vehicle.*.registered") {
		t.Errorf("unexpected subject %q", pub.Subjects()[0])
	}
	var body messages.NathejkVehicleRegistered
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.VehicleID != id || body.LicensePlate != "DK+AB12345" || body.CustodianUserId != "user-1" {
		t.Errorf("unexpected body %+v", body)
	}
}

func TestRegisterRequiresPlateCustodianAndYear(t *testing.T) {
	for _, tc := range []struct {
		name   string
		year   types.YearSlug
		mutate func(*RegisterFields)
	}{
		{"no year", "", func(*RegisterFields) {}},
		{"no plate", "2026", func(f *RegisterFields) { f.LicensePlate = "" }},
		{"no custodian", "2026", func(f *RegisterFields) { f.CustodianUserID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, pub := newTestCommander()
			f := registerFields()
			tc.mutate(&f)
			if _, err := c.Register(context.Background(), tc.year, f); err == nil {
				t.Fatal("want an error")
			}
			if len(pub.Messages) != 0 {
				t.Errorf("nothing should be published, got %v", pub.Subjects())
			}
		})
	}
}

// The rule the domain asks for: whoever brings the car drives it to begin with.
// It lives in the projector, so that it holds for any producer of the registered
// event rather than only for vehicle.Commands.
func TestProjectorMakesCustodianTheFirstDriver(t *testing.T) {
	w := &cqrstest.Writer{}
	con := &consumer{w: w}

	m := cqrstest.NewMessage(cqrs.SubjectFromStr("NATHEJK.2026.vehicle.vehicle-1.registered"))
	if err := m.SetBody(&messages.NathejkVehicleRegistered{
		VehicleID:       "vehicle-1",
		LicensePlate:    "DK+AB12345",
		CustodianUserId: "user-1",
	}); err != nil {
		t.Fatalf("set body: %v", err)
	}
	if err := con.HandleMessage(m); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	stmt := w.Last()
	if !strings.Contains(stmt, `'user-1'`) {
		t.Fatalf("statement should carry the custodian, got %q", stmt)
	}
	// Both columns are written from the same value on insert.
	if strings.Count(stmt, `'user-1'`) < 2 {
		t.Errorf("custodian should also be the first driver, got %q", stmt)
	}
	if !strings.Contains(stmt, "driverUserId") {
		t.Errorf("statement should set driverUserId, got %q", stmt)
	}
}

// A re-registration must not stand down a driver who was assigned later, so
// driverUserId and sectionSlug stay out of the upsert's update clause.
func TestProjectorDoesNotResetAssignmentsOnReRegistration(t *testing.T) {
	w := &cqrstest.Writer{}
	con := &consumer{w: w}

	m := cqrstest.NewMessage(cqrs.SubjectFromStr("NATHEJK.2026.vehicle.vehicle-1.registered"))
	if err := m.SetBody(&messages.NathejkVehicleRegistered{
		VehicleID:       "vehicle-1",
		LicensePlate:    "DK+AB12345",
		CustodianUserId: "user-1",
	}); err != nil {
		t.Fatalf("set body: %v", err)
	}
	if err := con.HandleMessage(m); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	stmt := w.Last()
	_, update, found := strings.Cut(stmt, "ON DUPLICATE KEY UPDATE")
	if !found {
		t.Fatalf("registered should upsert, got %q", stmt)
	}
	if strings.Contains(update, "driverUserId") {
		t.Errorf("update clause must leave the driver alone, got %q", update)
	}
	if strings.Contains(update, "sectionSlug") {
		t.Errorf("update clause must leave the section alone, got %q", update)
	}
}

// Update is a delta: an unset field must not appear in the statement at all,
// while a pointer to the zero value must, because that is how a field is
// cleared.
func TestProjectorUpdateWritesOnlyNamedFields(t *testing.T) {
	w := &cqrstest.Writer{}
	con := &consumer{w: w}

	empty := ""
	color := "blue"
	m := cqrstest.NewMessage(cqrs.SubjectFromStr("NATHEJK.2026.vehicle.vehicle-1.updated"))
	if err := m.SetBody(&messages.NathejkVehicleUpdated{
		VehicleID:   "vehicle-1",
		Color:       &color,
		Description: &empty,
	}); err != nil {
		t.Fatalf("set body: %v", err)
	}
	if err := con.HandleMessage(m); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	stmt := w.Last()
	if !strings.Contains(stmt, "color") || !strings.Contains(stmt, `'blue'`) {
		t.Errorf("want the color set, got %q", stmt)
	}
	if !strings.Contains(stmt, "description") {
		t.Errorf("a pointer to the empty string must clear the field, got %q", stmt)
	}
	if strings.Contains(stmt, "licensePlate") || strings.Contains(stmt, "brand") {
		t.Errorf("untouched fields must not appear, got %q", stmt)
	}
}

func TestUpdateWithNothingSetPublishesNothing(t *testing.T) {
	c, pub := newTestCommander()

	if err := c.Update(context.Background(), "2026", "vehicle-1", UpdateFields{}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("an empty delta says nothing and should publish nothing, got %v", pub.Subjects())
	}
}

func TestAssignDriverSkipsWhenUnchanged(t *testing.T) {
	c, pub := newTestCommander()
	c.q = fakeQueries{vehicle: &Vehicle{VehicleID: "vehicle-1", DriverUserID: "user-2"}}

	if err := c.AssignDriver(context.Background(), "2026", "vehicle-1", "user-2"); err != nil {
		t.Fatalf("AssignDriver: %v", err)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("re-assigning the same driver should publish nothing, got %v", pub.Subjects())
	}

	if err := c.AssignDriver(context.Background(), "2026", "vehicle-1", "user-3"); err != nil {
		t.Fatalf("AssignDriver: %v", err)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 event for a real change, got %d", len(pub.Messages))
	}
	if !pub.Messages[0].Subject().Match("NATHEJK.2026.vehicle.*.driver.assigned") {
		t.Errorf("unexpected subject %q", pub.Subjects()[0])
	}
}

// A vehicle that is not projected yet must still be assignable: the read model
// is eventually consistent, and refusing would be the wrong answer.
func TestAssignDriverToUnprojectedVehicle(t *testing.T) {
	c, pub := newTestCommander()
	c.q = fakeQueries{vehicle: nil}

	if err := c.AssignDriver(context.Background(), "2026", "vehicle-1", "user-2"); err != nil {
		t.Fatalf("AssignDriver: %v", err)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want the event published anyway, got %d", len(pub.Messages))
	}
}

func TestAssignSectionValidatesAndSkipsWhenUnchanged(t *testing.T) {
	c, pub := newTestCommander()
	c.q = fakeQueries{vehicle: &Vehicle{VehicleID: "vehicle-1", SectionSlug: "bandit"}}

	if err := c.AssignSection(context.Background(), "2026", "vehicle-1", "Not A Slug"); err == nil {
		t.Error("want an error for an invalid section slug")
	}
	if err := c.AssignSection(context.Background(), "2026", "vehicle-1", "bandit"); err != nil {
		t.Fatalf("AssignSection: %v", err)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("nothing should be published yet, got %v", pub.Subjects())
	}

	// An empty slug is how a vehicle is unassigned, and is not an invalid slug.
	if err := c.AssignSection(context.Background(), "2026", "vehicle-1", ""); err != nil {
		t.Fatalf("AssignSection: %v", err)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.Messages))
	}
	if !pub.Messages[0].Subject().Match("NATHEJK.2026.vehicle.*.section.assigned") {
		t.Errorf("unexpected subject %q", pub.Subjects()[0])
	}
}

func TestDeletePublishesDeleted(t *testing.T) {
	c, pub := newTestCommander()

	if err := c.Delete(context.Background(), "2026", "vehicle-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.Messages))
	}
	if !pub.Messages[0].Subject().Match("NATHEJK.2026.vehicle.*.deleted") {
		t.Errorf("unexpected subject %q", pub.Subjects()[0])
	}
}

// Deleting stands the car down entirely: it cannot stay on a section's list or
// look like somebody's current ride.
func TestProjectorDeleteClearsAssignments(t *testing.T) {
	w := &cqrstest.Writer{}
	con := &consumer{w: w}

	m := cqrstest.NewMessage(cqrs.SubjectFromStr("NATHEJK.2026.vehicle.vehicle-1.deleted"))
	if err := m.SetBody(&messages.NathejkVehicleDeleted{VehicleID: "vehicle-1"}); err != nil {
		t.Fatalf("set body: %v", err)
	}
	if err := con.HandleMessage(m); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	stmt := w.Last()
	for _, want := range []string{"deleted", "driverUserId", "sectionSlug"} {
		if !strings.Contains(stmt, want) {
			t.Errorf("delete should touch %s, got %q", want, stmt)
		}
	}
}

// Re-saving a form whose values match what is recorded must publish nothing:
// the stream should not carry edits that edited nothing.
func TestUpdateSkipsWhenNothingChanged(t *testing.T) {
	c, pub := newTestCommander()
	plate, color, seats := "DK+AB12345", "red", uint(8)
	c.q = fakeQueries{vehicle: &Vehicle{
		VehicleID:    "vehicle-1",
		LicensePlate: plate,
		Color:        color,
		SeatCount:    seats,
	}}

	err := c.Update(context.Background(), "2026", "vehicle-1", UpdateFields{
		LicensePlate: &plate,
		Color:        &color,
		SeatCount:    &seats,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("an unchanged delta should publish nothing, got %v", pub.Subjects())
	}
}

// A whole form arrives but only one value differs, so only that one is published.
func TestUpdatePublishesOnlyTheChangedFields(t *testing.T) {
	c, pub := newTestCommander()
	plate, oldColor := "DK+AB12345", "red"
	c.q = fakeQueries{vehicle: &Vehicle{
		VehicleID:    "vehicle-1",
		LicensePlate: plate,
		Color:        oldColor,
		Brand:        "VW",
	}}

	newColor, brand := "blue", "VW"
	err := c.Update(context.Background(), "2026", "vehicle-1", UpdateFields{
		LicensePlate: &plate,    // unchanged
		Brand:        &brand,    // unchanged
		Color:        &newColor, // changed
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.Messages))
	}
	var body messages.NathejkVehicleUpdated
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Color == nil || *body.Color != "blue" {
		t.Errorf("the changed field must be carried, got %+v", body.Color)
	}
	if body.LicensePlate != nil || body.Brand != nil {
		t.Errorf("unchanged fields must be pruned, got plate=%v brand=%v", body.LicensePlate, body.Brand)
	}
}

// Clearing a field that is already empty changes nothing either.
func TestUpdateSkipsClearingAnAlreadyEmptyField(t *testing.T) {
	c, pub := newTestCommander()
	c.q = fakeQueries{vehicle: &Vehicle{VehicleID: "vehicle-1", Description: ""}}

	empty := ""
	if err := c.Update(context.Background(), "2026", "vehicle-1", UpdateFields{Description: &empty}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(pub.Messages) != 0 {
		t.Errorf("clearing an empty field is not a change, got %v", pub.Subjects())
	}
}

// Clearing a field that holds something is a change, and must survive the prune.
func TestUpdateClearingAPopulatedFieldIsAChange(t *testing.T) {
	c, pub := newTestCommander()
	c.q = fakeQueries{vehicle: &Vehicle{VehicleID: "vehicle-1", Description: "roof box"}}

	empty := ""
	if err := c.Update(context.Background(), "2026", "vehicle-1", UpdateFields{Description: &empty}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.Messages))
	}
	var body messages.NathejkVehicleUpdated
	if err := pub.Messages[0].Body(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Description == nil || *body.Description != "" {
		t.Errorf("want a pointer to the empty string so the projector clears it, got %v", body.Description)
	}
}

// Nothing to compare against: publish what was given rather than drop the edit.
func TestUpdateOnUnprojectedVehiclePublishesAsGiven(t *testing.T) {
	c, pub := newTestCommander()
	c.q = fakeQueries{vehicle: nil}

	color := "red"
	if err := c.Update(context.Background(), "2026", "vehicle-1", UpdateFields{Color: &color}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(pub.Messages) != 1 {
		t.Fatalf("want the event published anyway, got %d", len(pub.Messages))
	}
}
