package types

// VehicleKind distinguishes the things in the vehicle inventory.
//
// The inventory serves two purposes at once, and this is what keeps them apart. It
// answers "what is in the race area" — for parking, access and insurance, which is
// why a trailer belongs in it at all — and it answers "which cars can collect a
// member off the route", which is a strictly smaller set. Without a kind, the
// second question has no honest answer: a coordinator looking for a car to
// dispatch would be offered trailers.
//
// A named type with a Valid() method rather than a bare string, shaped like
// MemberStatus. That is deliberate: an unknown value is then *detectable* instead
// of merely unexpected, which is the lesson PRD 006 §8 drew from section slugs
// arriving as free text.
type VehicleKind string

const (
	// VehicleKindCar is a car: it can be driven, and if it has seats it can be
	// sent to collect a member. The default, and what every vehicle registered
	// before this field existed is.
	VehicleKindCar VehicleKind = "car"

	// VehicleKindTrailer is a trailer. It counts as a vehicle in its own right —
	// its own registration, its own row — because that is what makes the site
	// inventory countable. It is never dispatched: the pickup pool is
	// kind = car AND seatCount > 0.
	VehicleKindTrailer VehicleKind = "trailer"
)

// Valid reports whether the kind is one this version knows.
//
// The empty string is **not** valid, and that asymmetry is the point: an event
// that omits the field is defaulted to car by the projector, so a kind that
// reaches a caller empty means something went wrong rather than "unspecified".
func (k VehicleKind) Valid() bool {
	switch k {
	case VehicleKindCar, VehicleKindTrailer:
		return true
	}
	return false
}

// OrCar returns the kind, defaulting an empty one to car.
//
// Exists so the default lives in exactly one place. Every historical
// vehicle.registered event predates this field, and projections are rebuilt by
// replaying the log — so the defaulting has to happen where the event is applied,
// not only in the column definition. A DEFAULT 'car' on the column is overwritten
// by an INSERT that names the column with an empty value, which is precisely what
// a replay of an old event would do.
func (k VehicleKind) OrCar() VehicleKind {
	if k == "" {
		return VehicleKindCar
	}
	return k
}
