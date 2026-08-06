package signup

import (
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"

	_ "embed"
)

type Signup struct {
	TeamID       types.TeamID        `json:"teamId"`
	Year         types.YearSlug      `json:"year"`
	TeamType     types.TeamType      `json:"teamType"`
	Name         string              `json:"name"`
	Email        *types.EmailAddress `json:"email"`
	EmailPending types.EmailAddress  `json:"emailPending"`
	Phone        *types.PhoneNumber  `json:"phone"`
	PhonePending types.PhoneNumber   `json:"phonePending"`
	Pincode      string              `json:"-"`
	Secret       string              `json:"-"`
	CreatedAt    string              `json:"createdAt"`
}

type table struct {
	commander
	consumer
	querier
}

func New(p cqrs.Publisher, w cqrs.Writer, r cqrs.Reader, services ...service) *table {
	q := querier{db: r}
	c := commander{p: p, q: &q, r: NewRepository(services...)}
	table := &table{commander: c, consumer: consumer{w: w}, querier: q}
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
