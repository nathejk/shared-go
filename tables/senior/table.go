package senior

import (
	"log"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"

	_ "embed"
)

type Senior struct {
	MemberID   types.MemberID     `json:"memberId"`
	YearSlug   types.YearSlug     `json:"year"`
	TeamID     types.TeamID       `json:"teamId"`
	ArmNumber  string             `json:"armNumber"`
	Name       string             `json:"name"`
	Address    string             `json:"address"`
	PostalCode string             `json:"postalCode"`
	City       string             `json:"city"`
	Email      types.EmailAddress `json:"email"`
	Phone      types.PhoneNumber  `json:"phone"`
	Birthday   string             `json:"birthday"`
	TshirtSize string             `json:"tshirtSize"`
	Diet       string             `json:"diet"`
	CreatedAt  time.Time          `json:"createdAt"`
	UpdatedAt  time.Time          `json:"updatedAt"`
}

type table struct {
	consumer
	querier
}

func New(w cqrs.Writer, r cqrs.Reader) *table {
	table := &table{consumer: consumer{w: w}, querier: querier{db: r}}
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
