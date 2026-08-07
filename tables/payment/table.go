// Package payment is the payment entity: the read API, the projector that keeps
// the payment table in sync with the payment events on the stream, and the
// write-side commands that drive a payment provider.
//
// # Consolidation
//
// This package merges two copies that had drifted apart — shared-go's
// tables/payment and the one in hq — keeping what each got right:
//
// From shared-go:
//   - the write side (commands.go) and the Provider port (interfaces.go), which
//     is what lets the domain talk about payments without naming MobilePay;
//   - AmountPaidByTeamID, and the LEFT JOIN on orders that makes any
//     team-scoped read find payments made against an order rather than
//     directly against the team.
//
// From hq:
//   - the operations audit trail: one JSON entry per state transition, with its
//     amount and time, so a partially captured payment can be reconstructed;
//   - subject patterns that are not pinned to a single year — the shared copy
//     subscribed to NATHEJK.2026.* and would have silently stopped projecting
//     on 1 January 2027;
//   - a projector that returns its errors instead of calling log.Fatalf, so a
//     bad statement is dead-lettered rather than taking the process down;
//   - context-aware queries and a Filter-driven GetAll.
//
// Deliberate divergence from both: amounts are in the currency's minor unit
// (øre) everywhere. The old queries applied FLOOR(amount/100) to hand callers
// DKK, which disagreed with the order entity, with the events on the stream and
// with the amount column itself, and hq's newer GetAll had already dropped it —
// leaving one package returning two different units. See the Amount fields.
package payment

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log"

	"github.com/doug-martin/goqu/v9"
	// Registers the MySQL dialect with goqu. Without this blank import
	// goqu.New("mysql", …) silently falls back to default dialect options and
	// quotes identifiers as "payment", which MariaDB reads as a string literal
	// and rejects. The failure is at query time, not build time.
	_ "github.com/doug-martin/goqu/v9/dialect/mysql"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"

	_ "embed"
)

// Operation is one state transition of a payment, as recorded on the payment's
// operations trail. Amount is in the currency's minor unit.
type Operation struct {
	Type   types.PaymentStatus `json:"type"`
	Amount int                 `json:"amount"`
	Time   string              `json:"time"`
}

// OperationList is the payment's transition history, stored as a JSON column.
type OperationList []Operation

func (o *OperationList) Scan(value any) error {
	if value == nil {
		*o = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("OperationList.Scan: unsupported type %T", value)
	}
	return json.Unmarshal(bytes, o)
}

func (o OperationList) Value() (driver.Value, error) {
	if o == nil {
		return "[]", nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Payment is a payment as projected from the stream.
//
// Amount is in the currency's minor unit (øre for DKK), matching the events,
// the column and the order entity.
//
// OrderForeignKey and OrderType keep their polymorphic names deliberately, even
// though Charge — the write side — now takes a plain OrderID. Across the
// projection they are not one thing: since the order entity landed
// OrderForeignKey is an order id and OrderType is "order", but the 769 rows that
// predate it hold a team or user id with OrderType naming the kind
// ("patrulje", "klan", "g\u00f8gler"). mobilepayCallbackHandler branches on exactly
// that to recover who paid. Renaming these to OrderID would make the field lie
// about most of the table.
type Payment struct {
	Reference       string              `json:"reference" db:"reference"`
	Year            string              `json:"year" db:"year"`
	ReceiptEmail    types.EmailAddress  `json:"receiptEmail" db:"receiptEmail"`
	ReturnUrl       string              `json:"returnUrl" db:"returnUrl"`
	Currency        types.Currency      `json:"currency" db:"currency"`
	Amount          int                 `json:"amount" db:"amount"`
	Method          string              `json:"method" db:"method"`
	Status          types.PaymentStatus `json:"status" db:"status"`
	CreatedAt       string              `json:"createdAt" db:"createdAt"`
	ChangedAt       string              `json:"changedAt" db:"changedAt"`
	OrderForeignKey string              `json:"orderForeignKey" db:"orderForeignKey"`
	OrderType       string              `json:"orderType" db:"orderType"`
	Operations      OperationList       `json:"operations" db:"operations"`
}

// table is the entity: read API, write API and projector.
type table struct {
	commander
	consumer
	querier
}

// New wires the payment entity and ensures its table exists.
//
// year is the season the published events belong to and appears in their
// subjects; the projector reads it back off the subject. Both merged copies
// hard-coded "2026" in three places instead.
//
// r is a cqrs.Reader rather than a *sql.DB so the read side stays mockable;
// cqrs.Reader happens to cover goqu's SQLDatabase method set exactly, so goqu
// can be built straight from it.
func New(p cqrs.Publisher, w cqrs.Writer, r cqrs.Reader, year types.YearSlug, es ...external) *table {
	q := querier{db: goqu.New("mysql", r)}
	// The commander is given the querier so it can verify a new reference is
	// unused; signup wires its commander the same way.
	c := commander{p: p, r: NewRepository(es...), q: &q, year: year}
	table := &table{commander: c, consumer: consumer{w: w}, querier: q}
	if err := w.Consume(table.CreateTableSql()); err != nil {
		log.Fatalf("Error creating table %q", err)
	}
	if err := w.Consume(addOperationsColumn); err != nil {
		log.Fatalf("Error migrating table %q", err)
	}
	return table
}

// addOperationsColumn brings an existing payment table up to the current
// schema. `operations` is newer than the table, and CREATE TABLE IF NOT EXISTS
// is a no-op wherever a payment table already exists — without this, every
// projection statement would fail on an unknown column and be dead-lettered.
//
// Issued as its own statement rather than appended to table.sql so it does not
// depend on the connection allowing multiple statements per Exec.
//
// IF NOT EXISTS on ADD COLUMN is a MariaDB extension, which is what this project
// runs. On MySQL this needs replacing with a real migration step.
const addOperationsColumn = "ALTER TABLE payment ADD COLUMN IF NOT EXISTS operations JSON NOT NULL DEFAULT ('[]');"

//go:embed table.sql
var tableSchema string

func (t *table) CreateTableSql() string {
	return tableSchema
}

// One value fills all three roles, which is what lets the composition root wire
// the same *table into the read models, the command bus and the consumer mux —
// and why there is no separate command constructor to get out of step with it.
var (
	_ Queries       = (*table)(nil)
	_ Commands      = (*table)(nil)
	_ cqrs.Consumer = (*table)(nil)
)
