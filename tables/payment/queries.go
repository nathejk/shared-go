package payment

import (
	"context"
	"database/sql"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// Aliases of the module-wide sentinels, so callers can write
// payment.ErrRecordNotFound. These MUST stay aliases: an errors.New copy would
// be a distinct value and errors.Is would silently stop matching.
var (
	ErrRecordNotFound = tables.ErrRecordNotFound
	ErrEditConflict   = tables.ErrEditConflict
)

// queryTimeout bounds every read. The projections are small and indexed; a read
// that takes longer than this is a symptom, not something to wait out.
const queryTimeout = 3 * time.Second

// The datasets below are built Prepared, so values travel as placeholder
// arguments rather than being interpolated into the statement. goqu does escape
// inlined literals correctly, but GetByReference's argument arrives straight
// from a URL path parameter and placeholders remove the question entirely.

type Queries interface {
	GetAll(context.Context, Filter) ([]Payment, error)
	GetByReference(context.Context, string) (*Payment, error)
	AmountPaid(context.Context, Filter) (int, error)
}

type querier struct {
	db *goqu.Database
}

// teamOwned is the predicate for "this payment belongs to one of these teams".
//
// It has to consider two shapes, because the linkage changed and the old rows
// are still there: legacy payments put the team id straight in
// payment.orderForeignKey, while current ones put an order id there and the
// team is the order's owner. Matching only the first — which the hq copy of
// this query did — finds nothing for any payment made since the order entity
// landed.
func teamOwned(teamIDs []types.TeamID) goqu.Expression {
	ids := make([]string, 0, len(teamIDs))
	for _, id := range teamIDs {
		ids = append(ids, string(id))
	}
	return goqu.Or(
		goqu.I("p.orderForeignKey").In(ids),
		goqu.I("o.ownerId").In(ids),
	)
}

// paymentColumns is spelled out rather than using SELECT *, so adding a column
// to the table cannot silently change what this returns.
var paymentColumns = []any{
	goqu.I("p.reference"), goqu.I("p.year"), goqu.I("p.receiptEmail"), goqu.I("p.returnUrl"),
	goqu.I("p.currency"), goqu.I("p.amount"), goqu.I("p.method"), goqu.I("p.status"),
	goqu.I("p.createdAt"), goqu.I("p.changedAt"), goqu.I("p.orderForeignKey"),
	goqu.I("p.orderType"), goqu.I("p.operations"),
}

// narrow applies a Filter to a dataset. Shared by GetAll and AmountPaid so the
// two cannot drift into disagreeing about what "this team, this year" means —
// which is how AmountPaidByTeamID came to have no year predicate at all while
// GetAll had one.
func narrow(ds *goqu.SelectDataset, f Filter) *goqu.SelectDataset {
	if f.Year != "" {
		ds = ds.Where(goqu.I("p.year").Eq(string(f.Year)))
	}
	if len(f.TeamIDs) > 0 {
		ds = ds.Where(teamOwned(f.TeamIDs))
	}
	return ds
}

// GetAll returns payments matching the filter, oldest first.
//
// An empty Filter matches everything, which is what the admin list wants; the
// year and team filters narrow it.
func (q *querier) GetAll(ctx context.Context, f Filter) ([]Payment, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var payments []Payment
	if err := q.allDataset(f).ScanStructsContext(ctx, &payments); err != nil {
		return nil, err
	}
	return payments, nil
}

// allDataset is separated from GetAll so the generated SQL can be asserted
// without a database. That is worth a method: the identifier quoting depends on
// the dialect being registered, and the team predicate is easy to get subtly
// wrong.
func (q *querier) allDataset(f Filter) *goqu.SelectDataset {
	ds := narrow(q.db.
		From(goqu.T("payment").As("p")).
		LeftJoin(goqu.T("orders").As("o"), goqu.On(goqu.I("o.orderId").Eq(goqu.I("p.orderForeignKey")))).
		Select(paymentColumns...).
		Prepared(true), f)
	// The join can duplicate a payment row only if two orders shared an id,
	// which the primary key forbids — so no DISTINCT is needed.
	return ds.Order(goqu.I("p.createdAt").Asc())
}

// GetByReference returns the single payment named by ref, or
// ErrRecordNotFound.
func (q *querier) GetByReference(ctx context.Context, ref string) (*Payment, error) {
	if ref == "" {
		return nil, ErrRecordNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var p Payment
	found, err := q.byReferenceDataset(ref).ScanStructContext(ctx, &p)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrRecordNotFound
	}
	return &p, nil
}

func (q *querier) byReferenceDataset(ref string) *goqu.SelectDataset {
	return q.db.
		From(goqu.T("payment").As("p")).
		Select(paymentColumns...).
		Prepared(true).
		Where(goqu.I("p.reference").Eq(ref))
}

// AmountPaid sums the payments actually secured — reserved or received — for
// whatever the filter selects, in the currency's minor unit.
//
// Takes a Filter rather than a bare team id, which is the fix for a real bug:
// the previous AmountPaidByTeamID had no year predicate, and team ids are UUIDs
// rather than year-scoped, so a team that signed up in two seasons had both
// seasons' payments summed together. One team in the live data already did, and
// its 2025 payment would have won it a 2026 team number. Callers must now say
// which year they mean; leaving Year empty still spans all of them, deliberately,
// for the admin totals.
//
// It returns an error rather than swallowing one as a zero: the caller uses the
// result to decide whether a team has paid (see assignNumberHandler), so a
// failed query reported as 0 is a team silently treated as unpaid.
func (q *querier) AmountPaid(ctx context.Context, f Filter) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var paid sql.NullInt64
	if _, err := q.amountPaidDataset(f).ScanValContext(ctx, &paid); err != nil {
		return 0, err
	}
	return int(paid.Int64), nil
}

func (q *querier) amountPaidDataset(f Filter) *goqu.SelectDataset {
	return narrow(q.db.
		From(goqu.T("payment").As("p")).
		LeftJoin(goqu.T("orders").As("o"), goqu.On(goqu.I("o.orderId").Eq(goqu.I("p.orderForeignKey")))).
		Select(goqu.COALESCE(goqu.SUM(goqu.I("p.amount")), 0)).
		Prepared(true).
		Where(goqu.I("p.status").In(string(types.PaymentStatusReserved), string(types.PaymentStatusReceived))), f)
}

// ConfirmBySecret is deliberately absent. shared-go's copy read a `confirm`
// table that no repo projects any more, and nothing called it — the live email
// verification path is signup.VerifyEmail, which reads signup.emailPending. See
// task 028.

var _ Queries = (*querier)(nil)
