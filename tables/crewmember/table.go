package crewmember

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
