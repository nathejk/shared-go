package vehicle

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/jrgensen/cqrs"
	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// queryTimeout bounds every read. The projection is small and indexed; a read
// slower than this is a symptom, not something to wait out.
const queryTimeout = 3 * time.Second

type Queries interface {
	GetByID(context.Context, types.VehicleID) (*Vehicle, error)
	GetAll(context.Context, Filter) ([]Vehicle, error)
}

type querier struct {
	db cqrs.Reader
	r  *goqu.Database
}

// vehicleColumns is spelled out rather than SELECT *, so adding a column cannot
// silently change what a read returns.
var vehicleColumns = []any{
	"vehicleId", "year", "licensePlate", "custodianUserId", "driverUserId",
	"sectionSlug", "color", "brand", "model", "seatCount", "description",
}

func (q *querier) GetByID(ctx context.Context, id types.VehicleID) (*Vehicle, error) {
	if id == "" {
		return nil, tables.ErrRecordNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var v Vehicle
	err := q.db.QueryRowContext(ctx,
		`SELECT vehicleId, year, licensePlate, custodianUserId, driverUserId,
			sectionSlug, color, brand, model, seatCount, description
		 FROM vehicle WHERE vehicleId = ? AND deleted = 0`,
		string(id),
	).Scan(&v.VehicleID, &v.YearSlug, &v.LicensePlate, &v.CustodianUserID, &v.DriverUserID,
		&v.SectionSlug, &v.Color, &v.Brand, &v.Model, &v.SeatCount, &v.Description)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, tables.ErrRecordNotFound
		}
		return nil, err
	}
	return &v, nil
}

// GetAll returns the vehicles matching the filter, by license plate so the list
// is stable between calls.
func (q *querier) GetAll(ctx context.Context, f Filter) ([]Vehicle, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	vehicles := []Vehicle{}
	if err := q.allDataset(f).ScanStructsContext(ctx, &vehicles); err != nil {
		return nil, err
	}
	return vehicles, nil
}

// allDataset builds GetAll's query.
//
// Split out from GetAll so the generated SQL can be asserted without a database,
// the way payment's query side does it: the filter fields are the part with rules
// worth pinning down — which of them narrow, which override each other, and
// which must travel as placeholders rather than be interpolated.
func (q *querier) allDataset(f Filter) *goqu.SelectDataset {
	where := goqu.Ex{"deleted": 0}
	if f.YearSlug != "" {
		where["year"] = string(f.YearSlug)
	}
	if f.SectionSlug != "" {
		where["sectionSlug"] = string(f.SectionSlug)
	}
	if f.Unassigned {
		where["sectionSlug"] = ""
	}
	if len(f.DriverUserIDs) > 0 {
		where["driverUserId"] = userIDStrings(f.DriverUserIDs)
	}
	if len(f.CustodianUserIDs) > 0 {
		where["custodianUserId"] = userIDStrings(f.CustodianUserIDs)
	}
	if f.LicensePlate != "" {
		where["licensePlate"] = f.LicensePlate
	}

	// Prepared: the filter values travel as placeholders rather than being
	// interpolated into the statement. GetByID has always used placeholders via
	// QueryRowContext; this side had not, and it now takes a value derived from a
	// caller's session, with a section slug and a plate plausibly arriving from a
	// request too.
	return q.r.From("vehicle").
		Prepared(true).
		Select(vehicleColumns...).
		Where(where).
		Order(goqu.I("licensePlate").Asc())
}

// userIDStrings converts ids for goqu, which takes a []string as an IN list but
// does not know what to do with a slice of a named string type.
func userIDStrings(ids []types.UserID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

var _ Queries = (*querier)(nil)
