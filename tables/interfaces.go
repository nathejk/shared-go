package tables

// This file declares what the entities in this package and its sub-packages
// require from the application, rather than importing it. Everything here is
// satisfied structurally by the concrete types in the consuming service, so
// nothing needs an adapter — see the interfaces.go files in the sub-packages
// for the ports specific to those entities.

// Validator collects validation failures under a key.
//
// It is deliberately narrower than the validator the application passes in,
// which also exposes Valid, AddError and CheckEmail: the Filter types here
// only ever record a failure, and deciding what to do about one is the
// caller's job. A single method also makes it trivial to substitute in a test.
type Validator interface {
	// Check records message under key when ok is false, and does nothing
	// otherwise.
	Check(ok bool, key, message string)
}

// PermittedValue reports whether value is one of permittedValues.
//
// Used to confirm a client-supplied sort column appears in the safelist before
// it reaches a query, since SortColumn interpolates it into SQL and panics
// rather than returning an error.
func PermittedValue[T comparable](value T, permittedValues ...T) bool {
	for i := range permittedValues {
		if value == permittedValues[i] {
			return true
		}
	}
	return false
}
