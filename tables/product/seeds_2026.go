package product

import "github.com/nathejk/shared-go/types"

// Seeds2026 returns the canonical product catalogue for the 2026 Nathejk.
//
// The values mirror the inline pricing that lived across the HTTP handlers
// before the order entity existed:
//
//   - klan / patrulje participation: 250 DKK per member
//   - crew participation: free (0 DKK) — crew are volunteers
//   - gøgler (badut) participation: 100 DKK per person
//   - t-shirt: 175 DKK, available to every owner type
//
// participation.klan carries Stock=115, which is the formal home of the
// 115-seat klan cap that used to live as klan.WithTotalMemberCount(115)
// in main.go. The klan commander reads this value through the product
// catalogue at request time; the order commander's checkStock enforces
// it independently when SetDerivedLines runs. Both cite the same source.
//
// All other products have nil stock (unlimited inventory).
//
// Adding a new product is a one-line append: rename / extend / retire by
// editing the slice and re-deploying — Seed is idempotent.
func Seeds2026() []Seed {
	year := "2026"
	klanCap := 115
	return []Seed{
		{
			SKU:         "participation.patrulje",
			Year:        year,
			Name:        "Patrulje-deltagelse",
			Kind:        KindParticipation,
			UnitPrice:   25000,
			EligibleFor: []types.TeamType{types.TeamTypePatrulje},
		},
		{
			SKU:         "participation.klan",
			Year:        year,
			Name:        "Senior-deltagelse",
			Kind:        KindParticipation,
			UnitPrice:   25000,
			EligibleFor: []types.TeamType{types.TeamTypeKlan},
			Stock:       &klanCap,
		},
		{
			SKU:         "participation.crew",
			Year:        year,
			Name:        "Crew-deltagelse",
			Kind:        KindParticipation,
			UnitPrice:   0,
			EligibleFor: []types.TeamType{types.TeamTypeCrew},
		},
		{
			SKU:         "participation.gogler",
			Year:        year,
			Name:        "Gøgler-deltagelse",
			Kind:        KindParticipation,
			UnitPrice:   10000,
			EligibleFor: []types.TeamType{types.TeamTypeBadut},
		},
		{
			SKU:       "tshirt.adult",
			Year:      year,
			Name:      "T-shirt",
			Kind:      KindMerchandise,
			UnitPrice: 17500,
			Sizes:     []string{"xs", "s", "m", "l", "xl", "xxl", "3xl"},
			// EligibleFor empty == EligibleAll; t-shirts are available to
			// patrulje, klan, crew and gøgler alike.
		},
	}
}
