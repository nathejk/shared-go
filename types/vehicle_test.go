package types

import "testing"

func TestVehicleKindValid(t *testing.T) {
	for _, k := range []VehicleKind{VehicleKindCar, VehicleKindTrailer} {
		if !k.Valid() {
			t.Errorf("%q should be valid", k)
		}
	}
	// The empty string is deliberately invalid: an event that omits the field is
	// defaulted at the projector, so a kind that reaches a caller empty is a fault,
	// not "unspecified".
	for _, k := range []VehicleKind{"", "lorry", "Car", "CAR"} {
		if k.Valid() {
			t.Errorf("%q should not be valid", k)
		}
	}
}

func TestVehicleKindOrCar(t *testing.T) {
	if got := VehicleKind("").OrCar(); got != VehicleKindCar {
		t.Errorf("empty should default to car, got %q", got)
	}
	if got := VehicleKindTrailer.OrCar(); got != VehicleKindTrailer {
		t.Errorf("a set kind must survive, got %q", got)
	}
	// Not a validator: an unknown value is passed through so it can be rejected
	// where the error can be reported, rather than silently becoming a car.
	if got := VehicleKind("lorry").OrCar(); got != VehicleKind("lorry") {
		t.Errorf("OrCar must not launder an unknown kind, got %q", got)
	}
}
