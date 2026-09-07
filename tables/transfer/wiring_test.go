package transfer_test

import (
	"testing"

	"github.com/nathejk/shared-go/tables/order"
	"github.com/nathejk/shared-go/tables/payment"
	"github.com/nathejk/shared-go/tables/spejder"
	"github.com/nathejk/shared-go/tables/transfer"
)

// The wiring in docs/moving-a-paid-member.md must actually compile.
//
// Each of the three read interfaces is satisfied by a value the composition root
// already has, and each package pins its own querier against its interface — but
// what a service holds is the *entity* returned by New, whose method set comes from
// an embedded querier. This asserts that last step, which is the one a caller
// discovers by being unable to wire the command.
//
// Never called: the point is the type check, and New would touch its nil
// dependencies.
func assertDocumentedWiring() {
	var (
		_ spejder.RosterReader   = spejder.New(nil, nil)
		_ order.MemberLineReader = order.New(nil, nil, nil, "2026", nil)
		_ payment.SourceResolver = payment.New(nil, nil, nil, "2026")
	)
}

func TestDocumentedWiringTypeChecks(t *testing.T) {
	// The assertion above is compile-time; this exists so the function is
	// referenced and cannot be deleted as dead code by a linter.
	_ = assertDocumentedWiring
	var _ transfer.Commands = transfer.New(nil, nil, nil, nil, "2026")
}
