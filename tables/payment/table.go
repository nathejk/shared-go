package payment

import (
	"log"

	"github.com/jrgensen/cqrs"

	_ "embed"
)

// table is the payment entity: the read API (Query) plus the projector that
// keeps the payment table in sync with the payment events on the stream.
type table struct {
	Query
	consumer
}

// New wires the payment entity and ensures its table exists.
func New(w cqrs.Writer, r cqrs.Reader) *table {
	table := &table{Query: Query{DB: r}, consumer: consumer{w: w}}
	if err := w.Consume(table.CreateTableSql()); err != nil {
		log.Fatalf("Error creating table %q", err)
	}
	return table
}

//go:embed table.sql
var tableSchema string

func (t *table) CreateTableSql() string {
	return tableSchema
}
