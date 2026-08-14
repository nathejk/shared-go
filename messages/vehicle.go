package messages

import "github.com/nathejk/shared-go/types"

// NathejkVehicleRegistered records a vehicle present in the race area.
//
// Every car that can be associated with the race in any way is registered, not
// only the ones on pickup duty: a crew member who drives in purely to transport
// themselves emits this too. The point is a complete picture of what is in the
// area — so a car can be accounted for, reached, or called on to collect a
// member off the route (see the transit and sheltered member states).
//
// One event when the vehicle first arrives for the season.
type NathejkVehicleRegistered struct {
	VehicleID types.VehicleID `json:"vehicleId"`

	// LicensePlate is the registration as written on the plate, prefixed with
	// the 2-letter ISO country code and a plus, e.g. "DK+AB12345". The prefix is
	// what disambiguates otherwise-identical plates from different countries.
	LicensePlate string `json:"licensePlate,omitempty"`

	// CustodianUserId is the person who currently holds and controls the
	// vehicle, including the authority to lend it out. Ownership of the
	// responsibility, not necessarily who is driving right now — that is the
	// driver, assigned separately (see NathejkVehicleDriverAssigned).
	CustodianUserId types.UserID `json:"custodianUserId,omitempty"`

	// Color, Brand and Model describe the vehicle well enough for a member or a
	// coordinator to recognise it at a pickup — "the red VW Transporter" — where
	// the plate alone is no help until it has already arrived.
	Color string `json:"color,omitempty"`
	Brand string `json:"brand,omitempty"`
	Model string `json:"model,omitempty"`

	// SeatCount is how many members the vehicle can carry, excluding the driver.
	// It bounds how many pickups a single car can be sent on before it has to
	// return to HQ. Zero for a car registered only for self-transport, which is
	// not offered for pickups.
	SeatCount uint `json:"seatCount,omitempty"`

	// Description is free-text for anything the structured fields do not cover —
	// a roof box, a trailer, "only available after midnight", and so on.
	Description string `json:"description,omitempty"`
}

// NathejkVehicleUpdated changes an already-registered vehicle. It is a delta:
// each field is a pointer, and the three states carry three distinct meanings.
//
//	nil (field omitted)     leave as it was
//	pointer to zero value   clear it ("color":"", "seatCount":0)
//	pointer to a value      set it
//
// This works because encoding/json's omitempty drops a pointer only when it is
// nil: a non-nil pointer is always emitted, even to the zero value, which is
// what lets "" mean clear rather than vanish. The cost is on the producer, which
// must allocate a pointer to signal a change (v.Color = &c) — assigning nothing
// leaves the field unchanged rather than erroring.
//
// VehicleID is not a pointer: it is the key, always required, never a change.
type NathejkVehicleUpdated struct {
	VehicleID types.VehicleID `json:"vehicleId"`

	LicensePlate    *string       `json:"licensePlate,omitempty"`
	CustodianUserId *types.UserID `json:"custodianUserId,omitempty"`
	Color           *string       `json:"color,omitempty"`
	Brand           *string       `json:"brand,omitempty"`
	Model           *string       `json:"model,omitempty"`
	SeatCount       *uint         `json:"seatCount,omitempty"`
	Description     *string       `json:"description,omitempty"`
}

// NathejkVehicleDriverAssigned puts a specific person behind the wheel of a
// vehicle for a run.
//
// Distinct from the custodian: the custodian answers for the vehicle across the
// season, while the driver is whoever is operating it now, and may change from
// one pickup to the next. Re-emitted on each reassignment; the latest wins.
type NathejkVehicleDriverAssigned struct {
	VehicleID    types.VehicleID `json:"vehicleId"`
	DriverUserID types.UserID    `json:"driverUserId"`
}

// NathejkVehicleDeleted withdraws a vehicle from the race: it is no longer
// available for pickups and should drop out of any list of usable cars. The
// runs it already made are unaffected.
type NathejkVehicleDeleted struct {
	VehicleID types.VehicleID `json:"vehicleId"`
}
