package vehicle

import (
	"github.com/nathejk/shared-go/types"
)

// Vehicle is the projection of a vehicle aggregate: a car present in the race
// area for one season.
//
// Every car associable with the race is here, not only the ones on pickup duty —
// a crew member who drives in purely to transport themselves registers too — so
// that the table answers "what is in the area" and not merely "what can we
// dispatch".
type Vehicle struct {
	VehicleID types.VehicleID `json:"vehicleId" db:"vehicleId"`
	YearSlug  types.YearSlug  `json:"yearSlug" db:"year"`

	// LicensePlate carries its country prefix, e.g. "DK+AB12345".
	LicensePlate string `json:"licensePlate" db:"licensePlate"`

	// CustodianUserID is who answers for the vehicle across the season,
	// including lending it out. DriverUserID is who is behind the wheel now,
	// which starts out as the custodian and changes as the car is handed on.
	CustodianUserID types.UserID `json:"custodianUserId" db:"custodianUserId"`
	DriverUserID    types.UserID `json:"driverUserId" db:"driverUserId"`

	// SectionSlug is the crew group the vehicle belongs to, empty when it is
	// not assigned to one.
	SectionSlug types.Slug `json:"sectionSlug" db:"sectionSlug"`

	Color string `json:"color" db:"color"`
	Brand string `json:"brand" db:"brand"`
	Model string `json:"model" db:"model"`

	// SeatCount excludes the driver: it is how many members the car can carry.
	SeatCount uint `json:"seatCount" db:"seatCount"`

	Description string `json:"description" db:"description"`
}

// Filter narrows GetAll. A zero Filter matches every vehicle that has not been
// deleted, across all seasons.
type Filter struct {
	// YearSlug matches the season the vehicle was registered for.
	YearSlug types.YearSlug

	// DriverUserIDs matches vehicles whose current driver is any of these
	// people — the cars a group can be reached through, e.g. everyone in a
	// section, or the drivers currently out on pickups.
	//
	// An empty slice does not filter; it is not the same as asking for
	// vehicles with no driver.
	DriverUserIDs []types.UserID
}
