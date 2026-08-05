package product

import (
	"fmt"
	"strings"
	"time"

	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/types"
)

// Seed describes a row of the catalogue intended to be inserted at startup.
//
// The zero value is not a valid product: at minimum SKU, Year, Name and
// Kind must be set. Currency defaults to "DKK" if empty. UnitPrice is in
// minor units (øre for DKK) and may be zero. Stock = nil means unlimited
// inventory. Active defaults to true unless explicitly set false.
//
// EligibleFor with len(0) means "every owner type"; pass {types.TeamTypePatrulje}
// etc. to constrain.
//
// Sizes lists the variant slugs the catalogue offers for this product
// (e.g. {"xs","s","m","l","xl","xxl"} for the t-shirt). Empty for
// products without size variants.
type Seed struct {
	SKU         string
	Year        string
	Name        string
	Kind        Kind
	UnitPrice   int
	Currency    string
	EligibleFor []types.TeamType
	Sizes       []string
	Stock       *int
	Active      *bool
}

type seeder struct {
	w cqrs.Writer
}

// Seed inserts the given products into the catalogue, updating any rows that
// already exist for the same (sku, year). Safe to call on every startup.
//
// Implementation note: product names and SKUs are passed through %q which
// is fine for this codebase's controlled seed data, matching how other
// projectors compose SQL. If/when an admin CRUD flow lands the values will
// move through proper parameter binding.
func (s *seeder) Seed(seeds []Seed) error {
	for _, p := range seeds {
		if p.SKU == "" || p.Year == "" {
			return fmt.Errorf("product seed missing sku or year: %+v", p)
		}
		currency := p.Currency
		if currency == "" {
			currency = "DKK"
		}
		active := 1
		if p.Active != nil && !*p.Active {
			active = 0
		}
		eligibleFor := EligibleAll
		if len(p.EligibleFor) > 0 {
			parts := make([]string, 0, len(p.EligibleFor))
			for _, t := range p.EligibleFor {
				parts = append(parts, string(t))
			}
			eligibleFor = strings.Join(parts, ",")
		}
		sizes := strings.Join(p.Sizes, ",")
		stockExpr := "NULL"
		if p.Stock != nil {
			stockExpr = fmt.Sprintf("%d", *p.Stock)
		}
		now := time.Now().Format(time.RFC3339)

		// Two-step ON DUPLICATE KEY pattern matches the rest of this
		// codebase. createdAt is set on insert and preserved on update.
		query := fmt.Sprintf(
			"INSERT INTO product SET sku=%q, year=%q, name=%q, kind=%q, unitPrice=%d, currency=%q, eligibleFor=%q, sizes=%q, stock=%s, active=%d, createdAt=%q, changedAt=%q "+
				"ON DUPLICATE KEY UPDATE name=VALUES(name), kind=VALUES(kind), unitPrice=VALUES(unitPrice), currency=VALUES(currency), eligibleFor=VALUES(eligibleFor), sizes=VALUES(sizes), stock=VALUES(stock), active=VALUES(active), changedAt=VALUES(changedAt)",
			p.SKU, p.Year, p.Name, string(p.Kind), p.UnitPrice, currency, eligibleFor, sizes, stockExpr, active, now, now,
		)
		if err := s.w.Consume(query); err != nil {
			return fmt.Errorf("seed product %s/%s: %w", p.SKU, p.Year, err)
		}
	}
	return nil
}
