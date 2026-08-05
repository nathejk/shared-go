package patrulje

import (
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"

	_ "embed"
)

type Patrulje struct {
	TeamID       types.TeamID       `json:"teamId"`
	TeamNumber   string             `json:"teamNumber"`
	Year         string             `json:"year"`
	Name         string             `json:"name"`
	Group        string             `json:"group"`
	Korps        string             `json:"korps"`
	Liga         string             `json:"liga"`
	ContactName  string             `json:"contactName"`
	ContactPhone types.PhoneNumber  `json:"contactPhone"`
	ContactEmail types.EmailAddress `json:"contactEmail"`
	ContactRole  string             `json:"contactRole"`
	MemberCount  int                `json:"memberCount"`
	TshirtCount  int                `json:"tshirtCount"`
	SignupStatus types.SignupStatus `json:"signupStatus"`
	PaidAmount   int                `json:"paidAmount"`
}

type table struct {
	commander
	consumer
	querier
}

func New(p cqrs.Publisher, w cqrs.Writer, r cqrs.Reader) *table {
	q := querier{db: r}
	c := commander{p: p, q: &q}
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
