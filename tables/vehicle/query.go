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
		ids := make([]string, 0, len(f.DriverUserIDs))
		for _, id := range f.DriverUserIDs {
			ids = append(ids, string(id))
		}
		where["driverUserId"] = ids
	}

	vehicles := []Vehicle{}
	err := q.r.From("vehicle").
		Select(vehicleColumns...).
		Where(where).
		Order(goqu.I("licensePlate").Asc()).
		ScanStructsContext(ctx, &vehicles)
	if err != nil {
		return nil, err
	}
	return vehicles, nil
}

var _ Queries = (*querier)(nil)
