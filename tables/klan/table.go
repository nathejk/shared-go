package klan

import (
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"

	_ "embed"
)

type Klan struct {
	ID                   types.TeamID       `json:"id"`
	Year                 types.YearSlug     `json:"year"`
	Status               types.SignupStatus `json:"status"`
	Name                 string             `json:"name"`
	Group                string             `json:"group"`
	Korps                string             `json:"korps"`
	MemberCount          int                `json:"memberCount"`
	RequestedMemberCount int                `json:"requestedMemberCount"`
	ReservedMemberCount  int                `json:"reservedMemberCount"`
	Lok                  string             `json:"lok"`
	PaidAmount           int                `json:"paidAmount"`
	Secret               string             `json:"-"`
	Pincode              string             `json:"-"`
}

/*
	type Klan2 struct {
		TeamID       types.TeamID       `sql:"teamId"`
		Year         string             `sql:"year"`
		Name         string             `sql:"name"`
		GroupName    string             `sql:"groupName"`
		Korps        string             `sql:"korps"`
		SignupStatus types.SignupStatus `sql:"signupStatus"`
	}
*/
type table struct {
	commander
	consumer
	querier
}

func New(p cqrs.Publisher, w cqrs.Writer, r cqrs.Reader, es ...external) *table {
	q := querier{db: r}
	c := commander{p: p, q: &q, r: NewRepository(es...)}
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
