package vehicle

import (
	"strings"
	"testing"

	"github.com/doug-martin/goqu/v9"
	"github.com/nathejk/shared-go/types"
)

// These tests assert the SQL the query side generates, without a database, the
// way payment's queries_test.go does.
//
// The filter is where the rules live: which fields narrow, which override each
// other, and — for the custodian filter this file was added for — that asking
// for a person's own vehicles is not quietly answered with the cars they happen
// to be driving.

func newTestQuerier() *querier {
	// goqu builds SQL without touching the connection, so a nil database is
	// fine as long as nothing is executed.
	return &querier{r: goqu.New("mysql", nil)}
}

func sqlOf(t *testing.T, f Filter) string {
	t.Helper()
	s, _, err := newTestQuerier().allDataset(f).ToSQL()
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	return s
}

// whereOf returns only the predicate.
//
// Asserting against the whole statement would be meaningless here, in two
// directions: every column this filters on is also in the SELECT list, and the
// ORDER BY names licensePlate unconditionally. So "does the SQL mention
// licensePlate" is true no matter what the filter did — which is how the first
// version of these tests managed to pass for the wrong reason.
func whereOf(t *testing.T, f Filter) string {
	t.Helper()
	s := sqlOf(t, f)
	_, where, found := strings.Cut(s, "WHERE")
	if !found {
		t.Fatalf("no WHERE clause in:\n%s", s)
	}
	if predicate, _, ordered := strings.Cut(where, "ORDER BY"); ordered {
		return predicate
	}
	return where
}

func argsOf(t *testing.T, f Filter) []any {
	t.Helper()
	_, args, err := newTestQuerier().allDataset(f).ToSQL()
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	return args
}

func TestCustodianFilterNarrowsOnCustodian(t *testing.T) {
	got := whereOf(t, Filter{CustodianUserIDs: []types.UserID{"u1"}})
	if !strings.Contains(got, "`custodianUserId`") {
		t.Errorf("expected a custodianUserId predicate, got:\n%s", got)
	}
	// The bug this guards against is the tempting substitution: answering "my
	// vehicles" with "vehicles I am driving". A person who lent their car out for
	// one pickup must not lose it from their own list, and the borrower must not
	// gain it — they would then hold edit and delete rights over somebody else's
	// registration.
	if strings.Contains(got, "`driverUserId`") {
		t.Errorf("a custodian filter must not touch driverUserId, got:\n%s", got)
	}
}

func TestSeveralCustodiansBecomeAnInList(t *testing.T) {
	got := sqlOf(t, Filter{CustodianUserIDs: []types.UserID{"u1", "u2"}})
	if !strings.Contains(got, "IN") {
		t.Errorf("expected an IN list for several custodians, got:\n%s", got)
	}
}

// An empty slice is "do not filter", not "vehicles with no custodian" — the same
// meaning DriverUserIDs documents, and the reason a zero value cannot express
// both (see Unassigned, which exists because of exactly that).
func TestEmptyCustodianSliceDoesNotFilter(t *testing.T) {
	got := whereOf(t, Filter{CustodianUserIDs: []types.UserID{}})
	if strings.Contains(got, "`custodianUserId`") {
		t.Errorf("an empty slice must not add a predicate, got:\n%s", got)
	}
}

// Both filters at once must AND rather than one replacing the other: "cars in
// this section that I answer for" is a legitimate question.
func TestCustodianCombinesWithOtherFilters(t *testing.T) {
	got := whereOf(t, Filter{
		YearSlug:         types.YearSlug("2026"),
		CustodianUserIDs: []types.UserID{"u1"},
		DriverUserIDs:    []types.UserID{"u2"},
	})
	for _, want := range []string{"`custodianUserId`", "`driverUserId`", "`year`"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %s in the predicate, got:\n%s", want, got)
		}
	}
}

// Deleted vehicles stay out however the filter is built: a withdrawn car must not
// reappear in its custodian's list.
func TestDeletedIsAlwaysExcluded(t *testing.T) {
	got := whereOf(t, Filter{CustodianUserIDs: []types.UserID{"u1"}})
	if !strings.Contains(got, "`deleted`") {
		t.Errorf("expected the deleted guard, got:\n%s", got)
	}
}

// goqu only emits backticks once the mysql dialect is registered; without the
// blank import in consumer.go it produces "vehicle", which MariaDB reads as a
// string literal. A compile cannot catch that.
func TestIdentifiersAreQuotedForMySQL(t *testing.T) {
	got := sqlOf(t, Filter{})
	if strings.Contains(got, `"`) {
		t.Errorf("identifiers are double-quoted, so the mysql dialect is not registered:\n%s", got)
	}
	if !strings.Contains(got, "`vehicle`") {
		t.Errorf("expected backtick-quoted identifiers, got:\n%s", got)
	}
}

// Values must not be interpolated into the statement. The custodian id comes from
// a session today, but a section slug or a plate can arrive from a request, and a
// query side that interpolates one value interpolates them all.
func TestFilterValuesTravelAsPlaceholders(t *testing.T) {
	f := Filter{CustodianUserIDs: []types.UserID{`x' OR 1=1 --`}}
	got := whereOf(t, f)
	if strings.Contains(got, "OR 1=1") {
		t.Errorf("the argument was interpolated into the statement:\n%s", got)
	}
	if !strings.Contains(got, "?") {
		t.Errorf("expected a placeholder, got:\n%s", got)
	}
	args := argsOf(t, f)
	found := false
	for _, a := range args {
		if a == `x' OR 1=1 --` {
			found = true
		}
	}
	if !found {
		t.Errorf("argument should travel separately, got %v", args)
	}
}

// A plate filter is what makes duplicate detection possible before a registration
// is published, so it must narrow on the plate and nothing else.
func TestLicensePlateFilterNarrowsOnThePlate(t *testing.T) {
	got := whereOf(t, Filter{LicensePlate: "DK+AB12345"})
	if !strings.Contains(got, "`licensePlate`") {
		t.Errorf("expected a licensePlate predicate, got:\n%s", got)
	}
	if !contains(argsOf(t, Filter{LicensePlate: "DK+AB12345"}), "DK+AB12345") {
		t.Error("the plate should travel as an argument")
	}
}

// An empty plate is "do not filter", as with every other field here. Getting this
// wrong would make a duplicate check match the vehicles with no plate at all
// rather than none.
func TestEmptyLicensePlateDoesNotFilter(t *testing.T) {
	got := whereOf(t, Filter{})
	if strings.Contains(got, "`licensePlate`") {
		t.Errorf("an empty plate must not add a predicate, got:\n%s", got)
	}
}

func contains(args []any, want any) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
