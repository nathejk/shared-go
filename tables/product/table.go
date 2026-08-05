// Package product is the read-model for the product catalogue. Each Nathejk
// year has its own catalogue: products are addressed by (sku, year).
//
// Products are not event-sourced for now — they are seeded at startup via
// Seed and can be re-seeded harmlessly (idempotent upsert). When admin CRUD
// is needed, this package can grow a commander/consumer pair without
// disturbing callers.
package product

import (
	"log"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"

	_ "embed"
)

// Kind classifies a product. Currently only used informationally (for
// rendering decisions on the frontend), but kept as a typed value so we
// don't sprinkle string literals around.
type Kind string

const (
	KindParticipation Kind = "participation"
	KindMerchandise   Kind = "merchandise"
)

// EligibleAll is the wildcard value stored in product.eligibleFor when a
// product can be bought by any owner type. The schema uses a comma-separated
// VARCHAR rather than JSON to stay consistent with the other tables in this
// codebase.
const EligibleAll = "*"

// Product is the in-memory representation of a row in the product table.
//
// UnitPrice is in the minor unit of Currency (øre for DKK), matching the
// payment / order events.
//
// Stock is a pointer so that NULL in the database (== unlimited inventory)
// stays distinguishable from a finite zero-stock value.
//
// EligibleFor lists the owner types (TeamType) that may add this product to
// an order. An empty slice or a slice equal to {EligibleAll} both mean
// "anyone".
//
// Sizes lists the variant slugs (e.g. "xs", "s", ..., "xxl" for the
// t-shirt) that the catalogue advertises for this product. Empty for
// products without size variants. The handler layer maps slugs to display
// labels; the catalogue is the source of truth for which slugs are
// currently offered.
type Product struct {
	SKU         string           `json:"sku"`
	Year        string           `json:"year"`
	Name        string           `json:"name"`
	Kind        Kind             `json:"kind"`
	UnitPrice   int              `json:"unitPrice"`
	Currency    string           `json:"currency"`
	EligibleFor []types.TeamType `json:"eligibleFor"`
	Sizes       []string         `json:"sizes,omitempty"`
	Stock       *int             `json:"stock,omitempty"`
	Active      bool             `json:"active"`
}

// IsEligibleFor reports whether the given owner type is allowed to buy this
// product.
func (p *Product) IsEligibleFor(ownerType types.TeamType) bool {
	if len(p.EligibleFor) == 0 {
		return true
	}
	for _, t := range p.EligibleFor {
		if string(t) == EligibleAll || t == ownerType {
			return true
		}
	}
	return false
}

type table struct {
	querier
	seeder
}

// New creates the product table (idempotently) and returns a value that
// exposes both the read API (Queries) and the seeding API (Seed).
//
// Self-healing schema: after creating the table it ensures any columns
// that have been added since the original schema landed are present.
// This is the codebase-wide complement to CREATE TABLE IF NOT EXISTS,
// which only handles initial creation.
func New(w cqrs.Writer, r cqrs.Reader) *table {
	t := &table{
		querier: querier{db: r},
		seeder:  seeder{w: w},
	}
	if err := w.Consume(tableSchema); err != nil {
		log.Fatalf("Error creating product table %q", err)
	}
	if err := cqrs.EnsureColumn(r, w, "product", "sizes",
		"sizes VARCHAR(255) NOT NULL DEFAULT '' AFTER eligibleFor"); err != nil {
		log.Fatalf("Error migrating product.sizes %q", err)
	}
	return t
}

//go:embed table.sql
var tableSchema string
