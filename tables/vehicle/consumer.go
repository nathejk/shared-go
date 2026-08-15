package vehicle

import (
	"log"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/mysql"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/messages"
)

type consumer struct {
	w cqrs.Writer
}

func (c *consumer) Consumes() []cqrs.Subject {
	return []cqrs.Subject{
		cqrs.SubjectFromStr("NATHEJK.*.vehicle.*.registered"),
		cqrs.SubjectFromStr("NATHEJK.*.vehicle.*.updated"),
		cqrs.SubjectFromStr("NATHEJK.*.vehicle.*.driver.assigned"),
		cqrs.SubjectFromStr("NATHEJK.*.vehicle.*.section.assigned"),
		cqrs.SubjectFromStr("NATHEJK.*.vehicle.*.deleted"),
	}
}

// HandleMessage projects a vehicle event.
//
// Only "registered" inserts. The other events are updates against a vehicleId
// that must already exist, because none of them carries enough to describe a
// car: an event stream is replayed in order, so the registration always lands
// first, and an update that matches no row is a vehicle that was never
// registered rather than one to invent from a fragment.
//
// Every branch returns its error instead of killing the process: the Writer is
// wrapped in a dead-letter writer whose whole purpose is to record a failing
// statement and carry on.
func (c *consumer) HandleMessage(msg cqrs.Message) error {
	dialect := goqu.Dialect("mysql")

	switch {
	case msg.Subject().Match("NATHEJK.*.vehicle.*.registered"):
		var body messages.NathejkVehicleRegistered
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.VehicleID == "" {
			return nil
		}
		// The year comes off the subject, not msg.Time(): the subject carries
		// the season the vehicle belongs to, which is the same value only until
		// a season is opened in the preceding calendar year.
		year := msg.Subject().Parts()[1]
		insert := goqu.Record{
			"vehicleId":       string(body.VehicleID),
			"year":            year,
			"licensePlate":    body.LicensePlate,
			"custodianUserId": string(body.CustodianUserId),
			// Whoever brings the car drives it until somebody else is assigned.
			// Applied here so it holds for any producer of this event, not only
			// vehicle.Commands.
			"driverUserId": string(body.CustodianUserId),
			"color":        body.Color,
			"brand":        body.Brand,
			"model":        body.Model,
			"seatCount":    body.SeatCount,
			"description":  body.Description,
			"deleted":      0,
		}
		// driverUserId and sectionSlug are absent from the update clause on
		// purpose: a re-registration must not undo a driver change or a section
		// assignment. The same reason crewmember's registered leaves
		// sectionSlug alone.
		update := goqu.Record{
			"year":            goqu.L("VALUES(year)"),
			"licensePlate":    goqu.L("VALUES(licensePlate)"),
			"custodianUserId": goqu.L("VALUES(custodianUserId)"),
			"color":           goqu.L("VALUES(color)"),
			"brand":           goqu.L("VALUES(brand)"),
			"model":           goqu.L("VALUES(model)"),
			"seatCount":       goqu.L("VALUES(seatCount)"),
			"description":     goqu.L("VALUES(description)"),
			"deleted":         0,
		}
		sqlStr, _, err := dialect.Insert("vehicle").Rows(insert).OnConflict(goqu.DoUpdate("vehicleId", update)).ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.vehicle.*.updated"):
		var body messages.NathejkVehicleUpdated
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.VehicleID == "" {
			return nil
		}
		// A delta: only the fields the event carries are written. A nil pointer
		// means "leave it"; a pointer to the zero value means "clear it", which
		// is why this reads the pointers rather than the values.
		set := goqu.Record{}
		if body.LicensePlate != nil {
			set["licensePlate"] = *body.LicensePlate
		}
		if body.CustodianUserId != nil {
			set["custodianUserId"] = string(*body.CustodianUserId)
		}
		if body.Color != nil {
			set["color"] = *body.Color
		}
		if body.Brand != nil {
			set["brand"] = *body.Brand
		}
		if body.Model != nil {
			set["model"] = *body.Model
		}
		if body.SeatCount != nil {
			set["seatCount"] = *body.SeatCount
		}
		if body.Description != nil {
			set["description"] = *body.Description
		}
		if len(set) == 0 {
			return nil
		}
		// Changing the custodian does not move the driver: the custodian may
		// well hand the keys on, which is what AssignDriver records.
		sqlStr, _, err := dialect.Update("vehicle").
			Set(set).
			Where(goqu.Ex{"vehicleId": string(body.VehicleID)}).
			ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.vehicle.*.driver.assigned"):
		var body messages.NathejkVehicleDriverAssigned
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.VehicleID == "" {
			return nil
		}
		// Overwriting is the point: a vehicle has one driver, so assigning a new
		// one silently stands the last one down. An empty id parks the car.
		sqlStr, _, err := dialect.Update("vehicle").
			Set(goqu.Record{"driverUserId": string(body.DriverUserID)}).
			Where(goqu.Ex{"vehicleId": string(body.VehicleID)}).
			ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.vehicle.*.section.assigned"):
		var body messages.NathejkVehicleSectionAssigned
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.VehicleID == "" {
			return nil
		}
		sqlStr, _, err := dialect.Update("vehicle").
			Set(goqu.Record{"sectionSlug": string(body.SectionSlug)}).
			Where(goqu.Ex{"vehicleId": string(body.VehicleID)}).
			ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	case msg.Subject().Match("NATHEJK.*.vehicle.*.deleted"):
		var body messages.NathejkVehicleDeleted
		if err := msg.Body(&body); err != nil {
			return err
		}
		if body.VehicleID == "" {
			return nil
		}
		// Soft delete, and the assignments go with it: a withdrawn car has no
		// driver on duty and belongs to no section. Keeping the row means a
		// replayed registration cannot resurrect it as available.
		sqlStr, _, err := dialect.Update("vehicle").
			Set(goqu.Record{"deleted": 1, "driverUserId": "", "sectionSlug": ""}).
			Where(goqu.Ex{"vehicleId": string(body.VehicleID)}).
			ToSQL()
		if err != nil {
			return err
		}
		return c.w.Consume(sqlStr)

	default:
		log.Printf("vehicle: unhandled message %q", msg.Subject().Subject())
	}
	return nil
}

var _ cqrs.Consumer = (*consumer)(nil)
