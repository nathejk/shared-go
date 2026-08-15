// Package tables holds the support code shared by the table entities in this
// module's sub-packages: the sentinel errors their read APIs return and the
// ports they require from the application.
//
// Each sub-package is a self-contained entity — a SQL projector, a read API
// and, where relevant, a command side and a saga:
//
//	crewmember  klan     order    patrulje  payment
//	product     section  senior   signup    spejder   vehicle
//
// They are shared verbatim between the Nathejk services, so they must not
// depend on anything outside this module: what an entity needs from the
// application it declares as an interface here or in its own interfaces.go.
package tables

import "errors"

var (
	ErrRecordNotFound     = errors.New("record not found")
	ErrEditConflict       = errors.New("edit conflict")
	ErrVerificationFailed = errors.New("failed verification")
)
