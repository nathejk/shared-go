package payment

import (
	"strings"
	"testing"

	"github.com/doug-martin/goqu/v9"
	"github.com/nathejk/shared-go/types"
)

// These tests assert the SQL the query side generates, without a database.
//
// Two bugs make that worth doing. First, goqu only quotes identifiers with
// backticks when the mysql dialect has been registered; without the blank import
// in table.go it emits `"payment"`, which MariaDB reads as a string literal and
// rejects — a runtime failure a compile cannot catch. Second, a team-scoped read
// has to consider both payment linkage shapes, and dropping the orders join
// silently returns zero rows for every payment made since the order entity
// landed, which is exactly what one of the two merged copies did.

func newTestQuerier() *querier {
	// goqu builds SQL without touching the connection, so a nil database is
	// fine as long as nothing is executed.
	return &querier{db: goqu.New("mysql", nil)}
}

func sqlOf(t *testing.T, ds *goqu.SelectDataset) string {
	t.Helper()
	s, _, err := ds.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	return s
}

// argsOf returns the placeholder arguments, which is where values live now that
// the datasets are Prepared.
func argsOf(t *testing.T, ds *goqu.SelectDataset) []any {
	t.Helper()
	_, args, err := ds.ToSQL()
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	return args
}

func contains(args []any, want any) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// Values must not be interpolated into the statement: GetByReference's argument
// comes from a URL path parameter.
func TestQueriesUsePlaceholders(t *testing.T) {
	q := newTestQuerier()
	ds := q.byReferenceDataset(`x' OR 1=1 --`)
	got := sqlOf(t, ds)
	if !strings.Contains(got, "`p`.`reference` = ?") {
		t.Errorf("reference should be a placeholder, got:\n%s", got)
	}
	if strings.Contains(got, "OR 1=1") {
		t.Errorf("the argument was interpolated into the statement:\n%s", got)
	}
	if args := argsOf(t, ds); !contains(args, `x' OR 1=1 --`) {
		t.Errorf("argument should travel separately, got %v", args)
	}
}

func TestIdentifiersAreQuotedForMySQL(t *testing.T) {
	got := sqlOf(t, newTestQuerier().allDataset(Filter{}))
	if strings.Contains(got, `"`) {
		t.Errorf("identifiers are double-quoted, so the mysql dialect is not registered:\n%s", got)
	}
	if !strings.Contains(got, "`payment`") {
		t.Errorf("expected backtick-quoted identifiers, got:\n%s", got)
	}
}

func TestGetAllSQL(t *testing.T) {
	q := newTestQuerier()

	for _, tc := range []struct {
		name   string
		filter Filter
		want   []string
		args   []any
	}{
		{
			name:   "no filter selects every payment",
			filter: Filter{},
			want: []string{
				"LEFT JOIN `orders` AS `o` ON (`o`.`orderId` = `p`.`orderForeignKey`)",
				"ORDER BY `p`.`createdAt` ASC",
			},
		},
		{
			name:   "year",
			filter: Filter{Year: types.YearSlug("2026")},
			want:   []string{"(`p`.`year` = ?)"},
			args:   []any{"2026"},
		},
		{
			// Both linkage shapes, or the team page shows nothing for
			// order-based payments.
			name:   "team matches direct and order-owned payments",
			filter: Filter{TeamIDs: []types.TeamID{"t-1"}},
			want: []string{
				"`p`.`orderForeignKey` IN (?)",
				"`o`.`ownerId` IN (?)",
				" OR ",
			},
			args: []any{"t-1"},
		},
		{
			name:   "year and teams combine",
			filter: Filter{Year: types.YearSlug("2026"), TeamIDs: []types.TeamID{"t-1", "t-2"}},
			want: []string{
				"(`p`.`year` = ?)",
				"`p`.`orderForeignKey` IN (?, ?)",
				" AND ",
			},
			args: []any{"2026", "t-1", "t-2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := q.allDataset(tc.filter)
			got := sqlOf(t, ds)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
			args := argsOf(t, ds)
			for _, want := range tc.args {
				if !contains(args, want) {
					t.Errorf("missing argument %v in %v", want, args)
				}
			}
		})
	}
}

// The column list is explicit so that adding a column to the table cannot
// change what callers receive. Assert every field the struct expects is asked
// for, since a missing one fails at scan time with a confusing error.
func TestGetAllSelectsEveryProjectedColumn(t *testing.T) {
	got := sqlOf(t, newTestQuerier().allDataset(Filter{}))
	if strings.Contains(got, "SELECT *") {
		t.Fatalf("SELECT * makes the read depend on the table's shape:\n%s", got)
	}
	for _, col := range []string{
		"reference", "year", "receiptEmail", "returnUrl", "currency", "amount",
		"method", "status", "createdAt", "changedAt", "orderForeignKey",
		"orderType", "operations",
	} {
		if !strings.Contains(got, "`p`.`"+col+"`") {
			t.Errorf("column %q is not selected:\n%s", col, got)
		}
	}
}

// Amounts stay in minor units. Both merged copies applied FLOOR(amount/100) in
// some queries and not others, so one package returned two different units.
func TestAmountsAreNotConvertedToDKK(t *testing.T) {
	q := newTestQuerier()
	for name, got := range map[string]string{
		"GetAll":         sqlOf(t, q.allDataset(Filter{})),
		"GetByReference": sqlOf(t, q.byReferenceDataset("ref-1")),
		"AmountPaid":     sqlOf(t, q.amountPaidDataset(Filter{})),
	} {
		if strings.Contains(strings.ToUpper(got), "FLOOR") {
			t.Errorf("%s divides the amount; minor units are the contract:\n%s", name, got)
		}
	}
}

func TestAmountPaidCountsOnlySecuredPayments(t *testing.T) {
	ds := newTestQuerier().amountPaidDataset(Filter{TeamIDs: []types.TeamID{"t-1"}})
	got := sqlOf(t, ds)
	if !strings.Contains(got, "COALESCE(SUM(`p`.`amount`), ?)") {
		t.Errorf("expected a COALESCEd sum (the 0 is a placeholder in prepared mode), got:\n%s", got)
	}
	// requested is not money in hand; reserved and received are.
	args := argsOf(t, ds)
	if !contains(args, "reserved") || !contains(args, "received") {
		t.Errorf("expected reserved/received filter, got args %v", args)
	}
	if contains(args, "requested") {
		t.Errorf("requested payments must not count as paid: args %v", args)
	}
	// Must consider both linkage shapes, like GetAll.
	if !strings.Contains(got, "LEFT JOIN `orders`") {
		t.Errorf("order-owned payments would be missed:\n%s", got)
	}
}

// A team id is a UUID, not year-scoped, so a team that signs up in two seasons
// collects payments under the same id. AmountPaid must therefore be able to
// narrow by year, and its filter must behave identically to GetAll's — the bug
// this replaces was precisely that the two disagreed.
func TestAmountPaidNarrowsByYearAndTeamLikeGetAll(t *testing.T) {
	q := newTestQuerier()
	f := Filter{Year: types.YearSlug("2026"), TeamIDs: []types.TeamID{"t-1"}}

	paid := sqlOf(t, q.amountPaidDataset(f))
	for _, want := range []string{
		"(`p`.`year` = ?)",
		"`p`.`orderForeignKey` IN (?)",
		"`o`.`ownerId` IN (?)",
	} {
		if !strings.Contains(paid, want) {
			t.Errorf("missing %q in:\n%s", want, paid)
		}
	}
	if args := argsOf(t, q.amountPaidDataset(f)); !contains(args, "2026") || !contains(args, "t-1") {
		t.Errorf("year and team should travel as arguments, got %v", args)
	}

	// An empty Year still spans every season, which is what the admin totals
	// want — but it must be an explicit choice, not the only behaviour.
	if got := sqlOf(t, q.amountPaidDataset(Filter{TeamIDs: []types.TeamID{"t-1"}})); strings.Contains(got, "`p`.`year`") {
		t.Errorf("an empty Year should not constrain the year:\n%s", got)
	}
}

func TestGetByReferenceRejectsEmptyReference(t *testing.T) {
	// Guarded before hitting the database: an empty reference would otherwise
	// scan an arbitrary row.
	if _, err := (&querier{}).GetByReference(t.Context(), ""); err != ErrRecordNotFound {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}
}
