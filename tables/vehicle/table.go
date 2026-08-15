// Package vehicle is the read model, command side and projector for the cars
// present in the race area.
//
// Every car associable with the race is registered, including one a crew member
// brings purely to transport themselves, so the table answers "what is in the
// area" rather than only "what can we dispatch". A vehicle has a custodian who
// answers for it across the season and a driver who is behind the wheel now —
// the custodian to begin with, then whoever the keys are handed to.
package vehicle

import (
	"log"

	"github.com/doug-martin/goqu/v9"
	"github.com/jrgensen/cqrs"

	_ "embed"
)

type table struct {
	commander
	consumer
	querier
}

// New wires the vehicle entity and ensures its table exists.
//
// The commander is given the querier so an assignment can tell a real change
// from a repeat of the one already recorded.
func New(p cqrs.Publisher, w cqrs.Writer, r cqrs.Reader) *table {
	q := querier{db: r, r: goqu.New("mysql", r)}
	t := &table{commander: commander{p: p, q: &q}, consumer: consumer{w: w}, querier: q}
	if err := w.Consume(t.CreateTableSql()); err != nil {
		log.Printf("Error creating table %q", err)
	}
	return t
}

//go:embed table.sql
var tableSchema string

func (t *table) CreateTableSql() string {
	return tableSchema
}

// One value fills all three roles, so the composition root can wire the same
// *table into the read models, the command bus and the consumer mux.
var (
	_ Queries       = (*table)(nil)
	_ Commands      = (*table)(nil)
	_ cqrs.Consumer = (*table)(nil)
)
