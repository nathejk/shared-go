package payment

import (
	"strings"
	"testing"

	"github.com/jrgensen/cqrs"
	"github.com/jrgensen/cqrs/cqrstest"
	"github.com/nathejk/shared-go/messages"
	"github.com/nathejk/shared-go/types"
)

// requestedStatement projects one payment.requested event and returns the
// statement the projector produced.
func requestedStatement(t *testing.T, body messages.NathejkPaymentRequested) string {
	t.Helper()
	w := &cqrstest.Writer{}
	c := &consumer{w: w}
	m := cqrstest.NewMessage(cqrs.SubjectFromStr("NATHEJK:2026.payment." + body.Reference + ".requested"))
	if err := m.SetBody(&body); err != nil {
		t.Fatalf("set body: %v", err)
	}
	if err := c.HandleMessage(m); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(w.Statements) != 1 {
		t.Fatalf("want 1 statement, got %d", len(w.Statements))
	}
	return w.Statements[0]
}

// A provider payment has no provenance because it *is* the provenance. The
// not-a-transfer pair must be written, so every row that predates the columns and
// every payment published by an older producer keeps its current meaning.
func TestProjectorWritesNoProvenanceForAProviderPayment(t *testing.T) {
	got := requestedStatement(t, messages.NathejkPaymentRequested{
		Reference: "AAAAAAAAAAAA",
		Method:    types.PaymentMethodMobilePay,
	})
	if !strings.Contains(got, `sourceReference=""`) {
		t.Errorf("want an empty sourceReference (not a transfer), got:\n%s", got)
	}
	if !strings.Contains(got, `source="{}"`) {
		t.Errorf("want an empty source record, got:\n%s", got)
	}
}

// A transfer records the root it moves money from, in both the indexed column and
// the snapshot.
func TestProjectorWritesProvenanceForATransfer(t *testing.T) {
	got := requestedStatement(t, messages.NathejkPaymentRequested{
		Reference: "CCCCCCCCCCCC",
		Method:    types.PaymentMethodInternalTransfer,
		Source: &types.PaymentSource{
			Reference: "AAAAAAAAAAAA",
			Via:       "BBBBBBBBBBBB",
			OwnerType: types.TeamTypePatrulje,
			OwnerID:   "team-1",
			Method:    types.PaymentMethodMobilePay,
			PaidAt:    "2026-06-04T12:00:00Z",
		},
	})
	if !strings.Contains(got, `sourceReference="AAAAAAAAAAAA"`) {
		t.Errorf("want the root in the indexed column, got:\n%s", got)
	}
	for _, want := range []string{"AAAAAAAAAAAA", "BBBBBBBBBBBB", "team-1", "2026-06-04T12:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot is missing %q in:\n%s", want, got)
		}
	}
	// The upsert must carry the new columns too, or a replayed requested event
	// would leave a row projected by an older build without provenance.
	if !strings.Contains(got, "sourceReference=VALUES(sourceReference)") || !strings.Contains(got, "source=VALUES(source)") {
		t.Errorf("ON DUPLICATE KEY UPDATE must refresh provenance, got:\n%s", got)
	}
}

// "We looked and could not tell" must survive into the column as something a
// query can find, not decay into "not a transfer".
func TestProjectorWritesUnknownProvenanceExplicitly(t *testing.T) {
	got := requestedStatement(t, messages.NathejkPaymentRequested{
		Reference: "CCCCCCCCCCCC",
		Method:    types.PaymentMethodInternalTransfer,
		Source:    types.UnknownPaymentSource(),
	})
	if !strings.Contains(got, `sourceReference="unknown"`) {
		t.Errorf("want an explicit unknown, got:\n%s", got)
	}
}

// A producer that says "this is a transfer" but leaves the reference empty must
// still be recorded as unknown: the three states may not collapse into two through
// an omission at the call site.
func TestProjectorNormalisesAnEmptySourceReferenceToUnknown(t *testing.T) {
	got := requestedStatement(t, messages.NathejkPaymentRequested{
		Reference: "CCCCCCCCCCCC",
		Method:    types.PaymentMethodInternalTransfer,
		Source:    &types.PaymentSource{OwnerID: "team-1"},
	})
	if !strings.Contains(got, `sourceReference="unknown"`) {
		t.Errorf("want an omitted reference normalised to unknown, got:\n%s", got)
	}
}

// The reserved and received branches are UPDATEs keyed on reference and must not
// touch the descriptive columns — which is why provenance is carried on requested.
func TestTransitionsDoNotTouchProvenance(t *testing.T) {
	w := &cqrstest.Writer{}
	c := &consumer{w: w}
	m := cqrstest.NewMessage(cqrs.SubjectFromStr("NATHEJK:2026.payment.AAAAAAAAAAAA.received"))
	if err := m.SetBody(&messages.NathejkPaymentReceived{Reference: "AAAAAAAAAAAA", Amount: 100}); err != nil {
		t.Fatalf("set body: %v", err)
	}
	if err := c.HandleMessage(m); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if got := w.Last(); strings.Contains(got, "source") {
		t.Errorf("a transition must not write provenance, got:\n%s", got)
	}
}
