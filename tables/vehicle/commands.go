package vehicle

import (
	"context"
	"errors"
	"fmt"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// Commands is the write-side interface for vehicles.
type Commands interface {
	Register(ctx context.Context, year types.YearSlug, f RegisterFields) (types.VehicleID, error)
	Update(ctx context.Context, year types.YearSlug, id types.VehicleID, f UpdateFields) error
	AssignDriver(ctx context.Context, year types.YearSlug, id types.VehicleID, driver types.UserID) error
	AssignSection(ctx context.Context, year types.YearSlug, id types.VehicleID, section types.Slug) error
	Delete(ctx context.Context, year types.YearSlug, id types.VehicleID) error
}

// RegisterFields is a new vehicle. A struct rather than a positional list
// because six of the fields are adjacent strings, where a transposition would
// compile and silently register the wrong car.
type RegisterFields struct {
	// LicensePlate identifies the physical car and is required: a vehicle
	// nobody can identify in the field is no use to a coordinator. Include the
	// country prefix, e.g. "DK+AB12345".
	LicensePlate string

	// CustodianUserID is who brings the car and answers for it. Required, and
	// it becomes the vehicle's first driver — see Register.
	CustodianUserID types.UserID

	Color string
	Brand string
	Model string

	// SeatCount excludes the driver. Zero for a car brought only for its
	// owner's own transport, which is not offered for pickups.
	SeatCount uint

	Description string
}

// UpdateFields is the editable slice of a vehicle carried by Update.
//
// Every field is a pointer, matching NathejkVehicleUpdated's delta semantics:
// nil leaves the field alone, a pointer to the zero value clears it, a pointer
// to a value sets it. That is why Update cannot be given a plain struct — with
// values there is no way to say "clear the description" and "leave the
// description" differently.
//
// Driver and section membership are deliberately absent: each has its own
// command and event, so editing details never clobbers an assignment.
type UpdateFields struct {
	LicensePlate    *string
	CustodianUserID *types.UserID
	Color           *string
	Brand           *string
	Model           *string
	SeatCount       *uint
	Description     *string
}

// isEmpty reports whether the delta would change nothing, so Update can skip
// publishing an event that says nothing.
func (f UpdateFields) isEmpty() bool {
	return f.LicensePlate == nil && f.CustodianUserID == nil && f.Color == nil &&
		f.Brand == nil && f.Model == nil && f.SeatCount == nil && f.Description == nil
}

// diff drops the fields that already hold the value being set, leaving only
// what actually changes.
//
// Callers hand Update a whole form, most of which is unchanged, so without this
// every save would publish every field and the stream would record edits that
// edited nothing — making the history useless for answering when a plate or a
// custodian really changed.
func (f UpdateFields) diff(v Vehicle) UpdateFields {
	if f.LicensePlate != nil && *f.LicensePlate == v.LicensePlate {
		f.LicensePlate = nil
	}
	if f.CustodianUserID != nil && *f.CustodianUserID == v.CustodianUserID {
		f.CustodianUserID = nil
	}
	if f.Color != nil && *f.Color == v.Color {
		f.Color = nil
	}
	if f.Brand != nil && *f.Brand == v.Brand {
		f.Brand = nil
	}
	if f.Model != nil && *f.Model == v.Model {
		f.Model = nil
	}
	if f.SeatCount != nil && *f.SeatCount == v.SeatCount {
		f.SeatCount = nil
	}
	if f.Description != nil && *f.Description == v.Description {
		f.Description = nil
	}
	return f
}

type commander struct {
	p cqrs.Publisher
	q Queries
}

// Register enrols a vehicle for the season and publishes
// NathejkVehicleRegistered, returning the id it minted.
//
// The custodian becomes the vehicle's first driver. That is applied by the
// projector when it inserts the row rather than published as a second event:
// custodianUserId is on the registered event, so every consumer can apply the
// same rule, and one event keeps a registration from half-succeeding. An
// explicit AssignDriver afterwards overrides it.
func (c commander) Register(ctx context.Context, year types.YearSlug, f RegisterFields) (types.VehicleID, error) {
	if !year.Valid() {
		return "", fmt.Errorf("invalid year slug %q", year)
	}
	if f.LicensePlate == "" {
		return "", errors.New("license plate is required")
	}
	if f.CustodianUserID == "" {
		return "", errors.New("custodian is required: somebody has to answer for the vehicle")
	}
	vehicleID := types.VehicleID("").New()
	body := messages.NathejkVehicleRegistered{
		VehicleID:       vehicleID,
		LicensePlate:    f.LicensePlate,
		CustodianUserId: f.CustodianUserID,
		Color:           f.Color,
		Brand:           f.Brand,
		Model:           f.Model,
		SeatCount:       f.SeatCount,
		Description:     f.Description,
	}
	msg := c.p.MessageFunc()(c.subject(year, vehicleID, "registered"))
	msg.SetBody(&body)
	if err := c.p.Publish(msg); err != nil {
		return "", err
	}
	return vehicleID, nil
}

// Update publishes NathejkVehicleUpdated with the fields that actually change.
//
// The delta is first compared against what is recorded and pruned to the
// differences, so re-saving an unchanged form publishes nothing and a one-field
// edit publishes one field. A vehicle that is not projected yet cannot be
// compared, so its delta is published as given — the read model is eventually
// consistent, and dropping the edit would be worse than repeating a value.
func (c commander) Update(ctx context.Context, year types.YearSlug, id types.VehicleID, f UpdateFields) error {
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if id == "" {
		return errors.New("vehicleId is required")
	}
	current, err := c.current(ctx, id)
	if err != nil {
		return err
	}
	if current != nil {
		f = f.diff(*current)
	}
	if f.isEmpty() {
		return nil
	}
	body := messages.NathejkVehicleUpdated{
		VehicleID:       id,
		LicensePlate:    f.LicensePlate,
		CustodianUserId: f.CustodianUserID,
		Color:           f.Color,
		Brand:           f.Brand,
		Model:           f.Model,
		SeatCount:       f.SeatCount,
		Description:     f.Description,
	}
	msg := c.p.MessageFunc()(c.subject(year, id, "updated"))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// AssignDriver puts a person behind the wheel, publishing
// NathejkVehicleDriverAssigned. Passing an empty user id parks the vehicle: it
// keeps its custodian but has nobody driving it.
//
// Assigning the driver a vehicle already has is a no-op, so re-submitting a
// dispatch form does not add a second identical event to the stream.
func (c commander) AssignDriver(ctx context.Context, year types.YearSlug, id types.VehicleID, driver types.UserID) error {
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if id == "" {
		return errors.New("vehicleId is required")
	}
	existing, err := c.current(ctx, id)
	if err != nil {
		return err
	}
	if existing != nil && existing.DriverUserID == driver {
		return nil
	}

	body := messages.NathejkVehicleDriverAssigned{VehicleID: id, DriverUserID: driver}
	msg := c.p.MessageFunc()(c.subject(year, id, "driver.assigned"))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// AssignSection places the vehicle under a crew group, publishing
// NathejkVehicleSectionAssigned. An empty slug unassigns it; assigning a
// different section implicitly unassigns the current one, since a vehicle
// belongs to at most one.
func (c commander) AssignSection(ctx context.Context, year types.YearSlug, id types.VehicleID, section types.Slug) error {
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if id == "" {
		return errors.New("vehicleId is required")
	}
	if section != "" && !section.Valid() {
		return fmt.Errorf("invalid section slug %q", section)
	}
	existing, err := c.current(ctx, id)
	if err != nil {
		return err
	}
	if existing != nil && existing.SectionSlug == section {
		return nil
	}

	body := messages.NathejkVehicleSectionAssigned{VehicleID: id, SectionSlug: section}
	msg := c.p.MessageFunc()(c.subject(year, id, "section.assigned"))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// Delete withdraws the vehicle from the race, publishing
// NathejkVehicleDeleted. A soft delete in the read model: the car drops out of
// every list, but the events that recorded its runs stay on the stream.
func (c commander) Delete(ctx context.Context, year types.YearSlug, id types.VehicleID) error {
	if !year.Valid() {
		return fmt.Errorf("invalid year slug %q", year)
	}
	if id == "" {
		return errors.New("vehicleId is required")
	}
	body := messages.NathejkVehicleDeleted{VehicleID: id}
	msg := c.p.MessageFunc()(c.subject(year, id, "deleted"))
	msg.SetBody(&body)
	return c.p.Publish(msg)
}

// current reads the vehicle so a command can tell a real change from a repeat of
// what is already recorded, tolerating one that is not projected yet.
//
// A missing row is not an error: the projection is eventually consistent, so a
// vehicle registered moments ago may not be readable, and refusing to assign a
// driver to it would be the wrong answer. Only the dirty checks need the row, so
// without it a command publishes what it was given.
func (c commander) current(ctx context.Context, id types.VehicleID) (*Vehicle, error) {
	if c.q == nil {
		return nil, nil
	}
	v, err := c.q.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, tables.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return v, nil
}

func (c commander) subject(year types.YearSlug, id types.VehicleID, event string) cqrs.Subject {
	return cqrs.SubjectFromStr(fmt.Sprintf("NATHEJK.%s.vehicle.%s.%s", year, id, event))
}

var _ Commands = commander{}
