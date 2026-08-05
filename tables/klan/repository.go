package klan

import (
	"github.com/nathejk/shared-go/tables/product"
)

// repository holds the klan commander's external dependencies and tunables.
//
// Capacity (the seat cap that used to be hard-coded at 115 via
// WithTotalMemberCount) now has two possible sources, in priority order:
//
//  1. Products — when set, RequestMemberCount looks up
//     participation.klan in the catalogue and uses its Stock field as
//     the cap. This is the canonical home of the cap going forward.
//
//  2. TotalMemberCount — the legacy fallback. Still honoured when no
//     product queries are wired (e.g. older tests) and as the value
//     returned when participation.klan exists but its Stock is NULL
//     (== unlimited).
//
// In production both wires line up with each other (catalogue carries
// 115, no legacy override). The dual-source design keeps the klan
// package usable in isolation for tests that don't want to set up the
// product catalogue.
type repository struct {
	Products           product.Queries
	TotalMemberCount   uint32
	TeamMinMemberCount uint32
	TeamMaxMemberCount uint32
}

type external func(*repository)

// WithProductQueries injects the product read API so RequestMemberCount
// can read the participation.klan cap from the catalogue. Pass the
// *table returned by product.New.
func WithProductQueries(q product.Queries) external {
	return func(r *repository) {
		r.Products = q
	}
}

func WithTeamMinMemberCount(val uint32) external {
	return func(r *repository) {
		r.TeamMinMemberCount = val
	}
}

func WithTeamMaxMemberCount(val uint32) external {
	return func(r *repository) {
		r.TeamMaxMemberCount = val
	}
}

// WithTotalMemberCount sets the legacy klan-wide seat cap. Prefer
// WithProductQueries plus a Stock value on participation.klan in the
// catalogue; this option remains for tests and for any deployment that
// hasn't migrated yet.
func WithTotalMemberCount(val uint32) external {
	return func(r *repository) {
		r.TotalMemberCount = val
	}
}

func NewRepository(es ...external) repository {
	r := repository{
		TeamMinMemberCount: 1,
		TeamMaxMemberCount: 4,
	}
	for _, with := range es {
		with(&r)
	}
	return r
}
