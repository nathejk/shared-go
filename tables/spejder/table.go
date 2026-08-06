package spejder

import (
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"

	_ "embed"
)

type Spejder struct {
	ID            types.MemberID     `json:"id"`
	MemberID      types.MemberID     `json:"memberId"`
	InitialTeamID types.TeamID       `json:"initialTeamId"`
	CurrentTeamID types.TeamID       `json:"currentTeamId"`
	Status        types.MemberStatus `json:"status"`
	Name          string             `json:"name"`
	Address       string             `json:"address"`
	PostalCode    string             `json:"postalCode"`
	City          string             `json:"city"`
	Email         string             `json:"email"`
	Phone         string             `json:"phone"`
	PhoneParent   string             `json:"phoneParent"`
	Birthday      types.Date         `json:"birthday"`
	Returning     bool               `json:"returning"`
	TShirtSize    string             `json:"tshirtSize"`
}

type table struct {
	consumer
	querier
}

func New(w cqrs.Writer, r cqrs.Reader) *table {
	table := &table{consumer: consumer{w: w}, querier: querier{db: r}}
	if err := w.Consume(table.CreateTableSql()); err != nil {
		log.Printf("Error creating table %q", err)
	}
	return table
}

//go:embed table.sql
var tableSchema string

func (t *table) CreateTableSql() string {
	return tableSchema
}
