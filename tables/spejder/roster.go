package spejder

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/nathejk/shared-go/tables"
	"github.com/nathejk/shared-go/types"
)

// RosterReader answers which team the roster currently has a member on.
//
// A narrow interface of its own rather than another method on Queries: Queries is
// implemented by fakes in other repos, and the caller that needs this — whoever
// reassigns a member between teams — needs nothing else from the read side.
type RosterReader interface {
	// TeamIDOf returns the team the member is on, or tables.ErrRecordNotFound if
	// the roster has no such member for that year.
	TeamIDOf(ctx context.Context, year types.YearSlug, memberID types.MemberID) (types.TeamID, error)
}

// TeamIDOf — see RosterReader.TeamIDOf.
//
// Scoped by year as well as member, matching the primary key: member ids are not
// year-scoped, so a member who took part twice has a row per season and an
// unscoped read would return whichever one the engine happened to find.
func (q querier) TeamIDOf(ctx context.Context, year types.YearSlug, memberID types.MemberID) (types.TeamID, error) {
	if memberID == "" {
		return "", tables.ErrRecordNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var teamID types.TeamID
	err := q.db.QueryRowContext(ctx,
		`SELECT teamId FROM spejder WHERE year = ? AND memberId = ?`,
		string(year), string(memberID)).Scan(&teamID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", tables.ErrRecordNotFound
	case err != nil:
		return "", err
	}
	return teamID, nil
}

var _ RosterReader = querier{}
