package payment

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/nathejk/shared-go/types"
)

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

// fakeLedger is a set of payment rows resolveSource can look up, plus a count of
// the lookups made — the count is what proves the root is reached in one hop
// rather than by walking a chain.
type fakeLedger struct {
	rows    map[string]sourceRow
	lookups []string
}

func (f *fakeLedger) lookup(ref string) (sourceRow, bool) {
	f.lookups = append(f.lookups, ref)
	row, ok := f.rows[ref]
	return row, ok
}

// providerRow is a real MobilePay payment linked through an order.
func providerRow(ref, ownerID string) sourceRow {
	return sourceRow{
		Reference:      ref,
		Method:         types.PaymentMethodMobilePay,
		CreatedAt:      "2026-06-04T12:00:00Z",
		OrderType:      OrderTypeOrder,
		OrderOwnerType: nullString(string(types.TeamTypePatrulje)),
		OrderOwnerID:   nullString(ownerID),
	}
}

// transferRow is an internal-transfer payment that already records its root.
func transferRow(ref, root string) sourceRow {
	return sourceRow{
		Reference:       ref,
		Method:          types.PaymentMethodInternalTransfer,
		CreatedAt:       "2026-06-05T12:00:00Z",
		SourceReference: root,
		OrderType:       OrderTypeOrder,
	}
}

// The ordinary case: money taken straight from a provider payment. That payment
// is its own root, and the owner comes off the order it was made for.
func TestResolveSourceNamesTheProviderPayment(t *testing.T) {
	l := &fakeLedger{rows: map[string]sourceRow{
		"AAAAAAAAAAAA": providerRow("AAAAAAAAAAAA", "team-1"),
	}}

	got := resolveSource("AAAAAAAAAAAA", l.lookup)
	if !got.Known() {
		t.Fatalf("source should be known, got %+v", got)
	}
	if got.Reference != "AAAAAAAAAAAA" || got.Via != "" {
		t.Errorf("reference/via = %q/%q, want the payment itself and no hop", got.Reference, got.Via)
	}
	if got.Method != types.PaymentMethodMobilePay || got.PaidAt == "" {
		t.Errorf("the snapshot should describe the real payment, got %+v", got)
	}
	if got.OwnerType != types.TeamTypePatrulje || got.OwnerID != "team-1" {
		t.Errorf("owner = %q/%q, want the order's owner", got.OwnerType, got.OwnerID)
	}
}

// The requirement that motivates recording the root rather than the predecessor:
// A → B → C must still name A. Anything else degrades provenance with every move
// and turns "who paid for this?" into a chain walk of unknown length.
func TestResolveSourceKeepsTheRootAcrossTwoHops(t *testing.T) {
	l := &fakeLedger{rows: map[string]sourceRow{
		"AAAAAAAAAAAA": providerRow("AAAAAAAAAAAA", "team-1"),
		// B moved A's money, so B records A as its root.
		"BBBBBBBBBBBB": transferRow("BBBBBBBBBBBB", "AAAAAAAAAAAA"),
	}}

	// C now moves B's money.
	got := resolveSource("BBBBBBBBBBBB", l.lookup)

	if got.Reference != "AAAAAAAAAAAA" {
		t.Errorf("reference = %q, want the root provider payment A", got.Reference)
	}
	if got.Via != "BBBBBBBBBBBB" {
		t.Errorf("via = %q, want the immediate predecessor B", got.Via)
	}
	// The descriptive fields must describe A, not B: it is A's money and A's payer.
	if got.Method != types.PaymentMethodMobilePay {
		t.Errorf("method = %q, want the root's method", got.Method)
	}
	if got.OwnerID != "team-1" {
		t.Errorf("owner = %q, want the root payer", got.OwnerID)
	}
	// Two reads: the payment, then its recorded root. Never a walk.
	if len(l.lookups) != 2 {
		t.Errorf("want the root reached in one extra read, got lookups %v", l.lookups)
	}
}

// A fourth hop must still name A, and must still cost one extra read — which is
// only true because every transfer stores the root rather than its predecessor.
func TestResolveSourceStaysFlatAcrossManyHops(t *testing.T) {
	l := &fakeLedger{rows: map[string]sourceRow{
		"AAAAAAAAAAAA": providerRow("AAAAAAAAAAAA", "team-1"),
		"BBBBBBBBBBBB": transferRow("BBBBBBBBBBBB", "AAAAAAAAAAAA"),
		"CCCCCCCCCCCC": transferRow("CCCCCCCCCCCC", "AAAAAAAAAAAA"),
	}}

	got := resolveSource("CCCCCCCCCCCC", l.lookup)
	if got.Reference != "AAAAAAAAAAAA" || got.Via != "CCCCCCCCCCCC" {
		t.Errorf("reference/via = %q/%q, want A via C", got.Reference, got.Via)
	}
	if len(l.lookups) != 2 {
		t.Errorf("want two reads however long the chain, got %v", l.lookups)
	}
}

// Unknown is a first-class outcome, not a failure. Every one of these is a real
// situation — a payment that is not projected, a reference nobody supplied, a
// transfer whose own origin was already unknown — and none may prevent a transfer
// from being created.
func TestResolveSourceIsExplicitlyUnknownWhenItCannotTell(t *testing.T) {
	l := &fakeLedger{rows: map[string]sourceRow{
		// A transfer whose own source could not be identified when it was made.
		"BBBBBBBBBBBB": transferRow("BBBBBBBBBBBB", types.PaymentSourceUnknown),
	}}

	for _, tc := range []struct {
		name      string
		reference string
		wantVia   string
	}{
		{"no reference given", "", ""},
		{"payment not projected", "ZZZZZZZZZZZZ", ""},
		{"origin was already unknown", "BBBBBBBBBBBB", "BBBBBBBBBBBB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSource(tc.reference, l.lookup)
			if got == nil {
				t.Fatal("provenance must never be nil: nil means \"not a transfer\"")
			}
			if got.Reference != types.PaymentSourceUnknown {
				t.Errorf("reference = %q, want %q", got.Reference, types.PaymentSourceUnknown)
			}
			if got.Known() {
				t.Error("an unidentified source must not report as known")
			}
			if got.Via != tc.wantVia {
				t.Errorf("via = %q, want %q", got.Via, tc.wantVia)
			}
		})
	}
}

// A root from a previous season is not in this projection, but its reference is
// still the most precise thing we have. Discarding it because the snapshot could
// not be filled in would lose the only durable part.
func TestResolveSourceKeepsAnUnprojectedRootsReference(t *testing.T) {
	l := &fakeLedger{rows: map[string]sourceRow{
		"BBBBBBBBBBBB": transferRow("BBBBBBBBBBBB", "AAAAAAAAAAAA"),
	}}

	got := resolveSource("BBBBBBBBBBBB", l.lookup)
	if got.Reference != "AAAAAAAAAAAA" || got.Via != "BBBBBBBBBBBB" {
		t.Errorf("reference/via = %q/%q, want the named root via B", got.Reference, got.Via)
	}
	if !got.Known() {
		t.Error("a named root is known even when its row is not here")
	}
}

// The legacy shape, which is the majority of the history: orderForeignKey holds a
// team id and orderType names the kind. For those payments the owner is the only
// thing knowable, so recovering it is what makes their provenance worth anything.
func TestResolveSourceRecoversTheOwnerOfALegacyPayment(t *testing.T) {
	l := &fakeLedger{rows: map[string]sourceRow{
		"AAAAAAAAAAAA": {
			Reference:       "AAAAAAAAAAAA",
			Method:          types.PaymentMethodMobilePay,
			CreatedAt:       "2025-06-04T12:00:00Z",
			OrderForeignKey: "team-legacy",
			OrderType:       string(types.TeamTypeKlan),
		},
	}}

	got := resolveSource("AAAAAAAAAAAA", l.lookup)
	if got.OwnerType != types.TeamTypeKlan || got.OwnerID != "team-legacy" {
		t.Errorf("owner = %q/%q, want the team the payment pointed straight at", got.OwnerType, got.OwnerID)
	}
}

// The lookup must be indexed and parameterised like every other read here.
func TestSourceRowDatasetSelectsProvenanceColumns(t *testing.T) {
	got := sqlOf(t, newTestQuerier().sourceRowDataset("AAAAAAAAAAAA"))
	for _, want := range []string{
		"`p`.`sourceReference`",
		"`orderOwnerId`",
		"LEFT JOIN `orders`",
		"`p`.`reference` = ?",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
}
