# shared-go

Shared Go source for the Nathejk services. Everything several of the services
would otherwise duplicate lives here: the domain vocabulary, the event payloads
on the wire, and the table entities that project those events into SQL.

```
module github.com/nathejk/shared-go
```

```
types/     # domain value types — Slug, TeamID, PhoneNumber, EmailAddress, …
messages/  # JetStream event payloads — the wire contract between services
tables/    # table entities — SQL projector + read API (+ commands, sagas)
```

There is no `cmd/`, no `main`, and no infrastructure here. This module is
imported, never run.

---

## Who consumes it

`tilmelding`, `hq`, `skan`, `hjælper`, `diplom` and `mobilepay` all require this
module. Not all of them use all three packages — `types` and `messages` are
near-universal, while `tables/` is aimed at the services that project the
Nathejk read models (`tilmelding`, `hq`, `skan`); their cutover from local
copies is still in progress, see [Origin of `tables/`](#origin-of-tables)
below.

Because the same struct is decoded by several services, **a change to
`messages/` is a change to a published contract**. Adding a field is safe;
renaming or retyping one breaks every service that has not been redeployed.
Prefer additive changes with `omitempty`.

---

## `types/`

Domain value types, mostly named string types with a `Valid()` method, plus a
few enums and time helpers:

- Identity: `ID`, `Slug`, `YearSlug`, `TeamID`, `MemberID`, `UserID`
- Contact: `PhoneNumber`, `EmailAddress` (both with normalisation/validation)
- Enums: `TeamType`, `SignupStatus`, `PaymentStatus`, `SectionType`, `CorpsSlug`,
  `Currency`
- Time: `Date`, `UnixtimeString`, `UnixtimeInteger` (the latter two convert to
  `*time.Time`, since the legacy monolith emits both shapes)
- Geo: `Latitude`, `Longitude`, `Position`

`Slug` and friends validate against a single regexp (lowercase, digits, single
interior hyphens). Use these types in signatures rather than bare `string` — the
compiler then catches a team ID passed where a user ID was meant.

## `messages/`

One file per aggregate (`team.go`, `member.go`, `crew.go`, `payment.go`,
`order.go`, `personnel.go`, `controlgroup.go`, …), each holding the structs
published to and consumed from JetStream. Names mirror the subject:
`NathejkOrderPaid`, `NathejkPaymentReceived`, `NathejkCrewMemberSectionAssigned`.

`meta.go` carries the envelope helpers (`MetaID`, `ByUserMeta`,
`MetadataRequestHeaders`) that the `metatagger` publisher attaches; `monolith.go`
holds the payloads still emitted by the legacy PHP monolith.

## `tables/`

One sub-package per entity. Each is a **self-contained slice of the read model**:
a `CREATE TABLE` in `table.sql`, a projector that consumes its own subjects, a
read API, and — where the entity is also written — a command side and sometimes
a saga.

| Entity | Constructor | Also has |
|---|---|---|
| `crewmember` | `New(p, w, r)` | commands, filter |
| `klan` | `New(p, w, r, es ...external)` | commands, `NewRepository` options |
| `order` | `New(p, w, r, year, products)` | `NewCommands`, `NewSaga` |
| `patrulje` | `New(p, w, r)` | commands, filter |
| `payment` | `New(p, w, r, year, es ...external)` | commands, `WithProvider` option, `Provider` port |
| `product` | `New(w, r)` | seeder (`seeds_2026.go`) |
| `section` | `New(p, w, r)` | commands, filter |
| `senior` | `New(w, r)` | filter |
| `signup` | `New(p, w, r, services ...service)` | commands, repository |
| `spejder` | `New(w, r)` | filter |
| `vehicle` | `New(p, w, r)` | commands, filter |

The root `tables` package holds only what the entities share:

- `errors.go` — `ErrRecordNotFound`, `ErrEditConflict`, `ErrVerificationFailed`.
  Compare with `errors.Is`; every entity returns these rather than its own.
- `interfaces.go` — the `Validator` port and `PermittedValue`, used by the
  `Filter` types to keep a client-supplied sort column off the SQL string.

### The cqrs seam

Entities never take a `*sql.DB`, a NATS connection or a concrete stream. They
take three interfaces from `github.com/jrgensen/cqrs`, which the consuming
service supplies from its `cmd/api` wiring:

| Interface | Role | Typical production implementation |
|---|---|---|
| `cqrs.Publisher` | command side — append events | `metatagger` over JetStream |
| `cqrs.Writer` | projection side — apply read-model SQL | `deadletter` over `sqlpersister` |
| `cqrs.Reader` | query side — read the read model | `*sql.DB` |

`cqrs` also supplies `Message`, `Subject`, `Consumer`, `SubjectFromStr` and the
`cqrstest` in-memory fakes, so `github.com/jrgensen/stream` need not be imported
here at all. Schema drift is handled with `cqrs.EnsureColumn` /
`cqrs.EnsureIndex` after the `CREATE TABLE IF NOT EXISTS` (MySQL/MariaDB only).

### Ports, not imports

When an entity needs something from the application, it declares the interface
it requires in an `interfaces.go` beside the entity files and the service
satisfies it. Existing ports:

- `tables.Validator` — request validation (satisfied structurally)
- `signup.Mailer`, `signup.SmsSender` — outbound notifications
- `payment.Provider` — a payment service provider in Nathejk's own vocabulary,
  adapted from e.g. MobilePay in the consuming service and injected with
  `payment.WithProvider`
- `order.PaymentReader` — the slice of the payment read API the order saga
  needs; `payment.Queries` satisfies it, pinned by an assertion in `order`

`klan`, `payment` and `signup` take their optional collaborators as variadic
option functions (`WithProductQueries`, `WithProvider`, `WithTeamMaxMemberCount`,
…) so a test can construct them with none.

### Rules for code under `tables/`

1. **No dependency outside this module's own packages** plus `cqrs`, `goqu`,
   `uuid`, `go-nanoid` and the standard library. An entity that imports a
   service's `internal/` tree cannot be shared — declare a port instead.
2. **An entity owns exactly its own table and subjects.** Cross-entity reads go
   through the other entity's exported `Queries` interface (see how `order` and
   `klan` consume `product.Queries`), not through a hand-written `JOIN` into a
   table you do not own. The one sanctioned exception is `payment`'s `LEFT JOIN`
   on `orders`: a payment links to its team either directly (legacy rows) or via
   the order's owner, and no read API can express "either shape" without the
   join. Document the reason in the query if you need another.
3. **Projectors are idempotent.** The stream is replayed on every startup, so
   every statement is `INSERT … ON DUPLICATE KEY UPDATE` or a guarded `UPDATE`.
4. **Keep the sentinels shared.** Return `tables.ErrRecordNotFound`, don't mint
   a package-local equivalent — callers compare across entity boundaries.

---

## Development

The services resolve this module through a Go workspace in dev: `go/go.work` in
e.g. `tilmelding` points at the sibling `../../shared-go` checkout, which is
bind-mounted into the `api` container at `/shared-go`. Edits here are therefore
picked up live by a running service, with no publish step.

That cuts both ways: **a change that only builds with the workspace active will
fail CI**, which runs with `GOWORK=off` against the version pinned in the
service's `go.mod`. There are no release tags — consumers pin pseudo-versions
(`v0.0.0-<timestamp>-<commit>`), so shipping a change is:

1. commit and push here;
2. in each consuming service, `go get github.com/nathejk/shared-go@main` (or the
   specific commit) and run its tests.

Keep `./...` green before pushing:

```sh
gofmt -l .
go build ./...
go vet ./...
go test ./...
```

The `go` directive is `1.25.0` — the floor set by `github.com/jrgensen/cqrs`.
A consuming service on an older Go line must bump before it can pull a version
of this module that includes `tables/`.

## Origin of `tables/`

The entities under `tables/` grew inside `tilmelding`'s `go/nathejk/table/`
tree and were moved here so `hq` and `skan` could stop keeping their own
near-copies. `tilmelding` remains the reference for anything not yet moved: its
root `table` package still holds legacy projectors (`confirm.go`, `registrant.go`,
`pincode.go`, the `*status.go` pairs, …) that have not been reshaped into
entities. When one of them moves, it moves the same way: a sub-package with
`table.go`, `table.sql`, a projector, a read API, and no import that points back
at a single service.
