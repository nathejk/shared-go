# 159 — [shared-go] Seat transfer command + pre-race member reassignment

**Status:** open
**Priority:** high
**Created:** 2026-09-07
**Picked up by:**
**Started:**
**Completed:**

> **This task is implemented in the `github.com/nathejk/shared-go` repo, not here.**
> It is tracked on this board because hq's PRD 012 depends on it. Lift the whole file
> into that repo (or hand it to an agent working there) — everything needed to do the
> work is below, with no reference to hq required.

**Depends on:** 156 (credit orders can settle), 157 (internal-transfer method),
158 (payment provenance). Do not start before those land — this task is their caller.

## Description

Two things that must happen together, and one operation that owns both.

### 1. A member's team can change before the race

`spejder.teamId` is currently **write-once**. The only branch that ever writes it is an
`INSERT IGNORE` in `tables/spejder/consumer.go`:

```go
	case msg.Subject().Match("nathejk.*.spejder.*.updated"):
		var legacy messages.NathejkMemberAdded
		if err := msg.Body(&legacy); err != nil {
			return err
		}
		if legacy.TeamID != "" {
			query := `INSERT IGNORE INTO spejder (memberId, year, teamId, createdAt) VALUES (%q,%q,%q,%q)`
```

and the `UPDATE spejder SET name=…, address=…` that follows it deliberately does not
include `teamId`. `Consumes()` is only:

```go
		cqrs.SubjectFromStr("NATHEJK.*.spejder.*.updated"),
		cqrs.SubjectFromStr("NATHEJK.*.spejder.*.deleted"),
		cqrs.SubjectFromStr("NATHEJK:*.patrulje.*.started"),
```

So **no event in the system moves a member between teams on the roster**. Re-emitting
`spejder.*.updated` with a different `teamId` is a no-op for the roster row.

There *is* a race-time move — `NathejkMemberTeamMoved` / `spejder.*.team.moved` — but it
is a different fact and must not be reused. Its own documentation says why:

> The member's status does not change: a survivor moved into another patrol is still
> racing and still self-carrying, so they can still finish — with a team that is not the
> one they started with, which is why `initialTeamId` is never overwritten.

That event describes somebody who **started with one patrol and continued with another**,
which is why the origin keeps them on its roster and `initialTeamId` is preserved. A
**pre-race** reassignment is the opposite: nothing has started, the member simply belongs
to the other team now, and the origin roster must no longer list them. Encoding it as
`team.moved` would make `initialTeamId` — the record of where somebody *started* — into a
lie.

**Add a distinct event**, proposed `NATHEJK.{year}.spejder.{memberId}.reassigned`, payload
parallel to `NathejkMemberTeamMoved` (`memberId`, `fromTeamId`, `toTeamId`, `actor`), so
the difference in meaning is carried by the subject rather than by a flag on a shared
struct. `tables/spejder/consumer.go` consumes it and **updates `teamId`**.

The status projection (`spejderstatus`, currently in hq) must **ignore** it: pre-race there
is no status row and no `activeMemberCount` to recompute, and writing `initialTeamId`
would be actively wrong. A reassigned member who later races has always been on their new
team.

### 2. The money follows the member

The member has been paid for. Their seat is a paid participation line, and they may also
have bought merchandise (a t-shirt). Moving them without moving the money leaves one team
paid up for somebody who left and the other short for somebody who arrived.

Add **one command** that performs the whole transfer, rather than leaving a caller to
orchestrate five:

1. Find the transferable lines: every `order_line` on a **paid** order owned by the origin,
   whose `memberId` is the moving member — participation **and** merchandise, at the
   `unitPrice` actually snapshotted on the line, not today's catalogue price.
2. Create a **credit order** owned by the origin (the same lines, negative) and a **charge
   order** owned by the destination (the same lines, positive). Every line must preserve
   `productSku`, `productName`, `unitPrice`, `memberId` and `attributes` — the t-shirt size
   lives in `attributes` and the order is authoritative for shipping, so losing it
   misdirects a physical object.
3. Cover both with **internal-transfer payments** (task 157's method), carrying
   **provenance** (task 158) so the money's original provider payment is still named.
4. Publish the **reassignment** from part 1.

**The pair must net to zero**: credit total + charge total == 0. That is the invariant that
makes the whole thing auditable, and the cheapest thing to test.

`EnsureOpenOrder` is explicitly **not** the entry point — it reuses an existing open order
and would contaminate a real, unpaid order with transfer lines. Transfer orders are
dedicated and immediately settled.

## Notes

### Ordering and failure semantics — the part to get right

There is no transaction across JetStream publishes, so decide what a partial failure
leaves behind, deliberately:

- Money moved, member did not → funds have moved for somebody who hasn't. Hard to spot.
- Member moved, money did not → a member sits on a team whose seat was never paid for,
  while the origin still holds it. Recoverable by hand, and **visible**.

So **publish the money first and the reassignment last**, and make the whole operation
idempotent on replay. Validate everything up front before publishing anything — the
existing `MoveMembers` command in the member-status package is the precedent: it
pre-validates every member, then publishes.

### Publishing the payments

The payment projector's `received` branch is an `UPDATE` keyed on `reference`, and only the
`requested` branch inserts the row. So a transfer payment needs **`payment.requested` then
`payment.received`** for the same reference — a lone `received` updates nothing and the
payment silently does not exist. `orderForeignKey` is the order id and `orderType` is
`"order"` (`OrderTypeOrder`), which is what makes the paid-amount subquery find it.

A payment must reach `reserved` or `received` to count: every paid-amount computation
filters `status IN ('reserved','received')`.

### Idempotency and replay

Read models are rebuilt from JetStream on every start, so a replay must not double-credit
anyone. Order ids, line ids and payment references must be **deterministic** functions of
the transfer (not fresh UUIDs generated at handling time), or the replay creates a second
transfer. Note the existing convention: derived line ids are
`"derived:{sku}:{memberId}"`-style precisely so projectors can upsert by
`(orderId, lineId)`, and `handlePaid` / `handleCancelled` guard their UPDATEs with
`AND status='open'`.

### Traps in the code you will touch

- **`tables/spejder/consumer.go` builds SQL with `fmt.Sprintf` and `%q`**, not
  placeholders (`c.w.Consume(fmt.Sprintf(query, args...))`). Match the surrounding style.
- **Its `UPDATE … WHERE memberId = %q` is not scoped by year**, although the table's
  primary key is `(year, memberId)`. Scope the new branch by **both**, or a reassignment in
  one season will rewrite the same member's row in another.
- **`Consumes()` uses `NATHEJK.` / `NATHEJK:` prefixes while the `Match` calls in
  `HandleMessage` are lowercase `nathejk.`** — an existing inconsistency in that file.
  Follow whatever actually matches at runtime and don't "tidy" it as part of this task.
- `tables/spejder/table.go`'s read struct already advertises `InitialTeamID`,
  `CurrentTeamID` and `Status` fields that the table has no columns for. Don't take them as
  evidence of a design to follow.

### Splitting this task

The roster event and the money command are separable, and it is tempting to split them. The
reason they are one task is the ordering requirement above: it can only be *enforced* by
whoever owns both publishes. If you do split it, keep one task owning the sequence and the
idempotency, and make the other strictly additive.

### Contract that hq will depend on

hq calls this command and projects the event. Record in the progress log:

- the exact **subject** for the reassignment and its **JSON field names**
- the exported **command signature** and every error it can return (hq maps errors to
  specific Danish validation messages, so distinguishable errors matter — e.g. "already on
  that team" must not be indistinguishable from "nothing to transfer")
- how a transfer's two orders are **linked and identified** as a transfer, so hq can label
  them and finance can pair them
- what happens when the member has **no paid seat**: expected behaviour is that the
  reassignment still succeeds and **no orders are created** (there is nothing to transfer),
  and hq needs to know so it can say so in the UI

**Preconditions are the caller's job, not this command's.** hq enforces "destination
accepted / not started / under 7 members" because it owns the team read models and the
operator-facing error messages. This command should refuse only what it alone can know:
same-team moves, and a member that does not exist. In particular **do not enforce a minimum
member count on the origin** — emptying a team out one member at a time is a required
capability, because a team below three is not allowed to start and its remaining members
must be transferable to a team that has not started.

### Impact on other consumers

This is the one task in the group that changes existing behaviour: `spejder.teamId` becomes
mutable. `spejder` is shared with tilmelding, which also creates members. The change is
narrow — a **new** subject, in a **new** branch, updating a column no other branch writes —
so nothing that works today behaves differently. Confirm nothing in tilmelding assumes the
roster's `teamId` is immutable before landing it; if something does, say so rather than
working around it.

## Acceptance Criteria

- [ ] A new pre-race reassignment event exists, distinct from `spejder.*.team.moved`, with
      `fromTeamId` / `toTeamId` / actor
- [ ] `tables/spejder/consumer.go` consumes it and updates `teamId`, scoped by **year and
      memberId**
- [ ] No other event can change `teamId`; the `spejder.*.updated` behaviour is unchanged
- [ ] The status projection ignores the new event; `initialTeamId` is never written by it
- [ ] One command performs the whole transfer: find lines, create both orders, cover both
      with internal-transfer payments carrying provenance, publish the reassignment last
- [ ] Transferable lines include **merchandise as well as participation**, at the price
      snapshotted on the original line, preserving `attributes` (t-shirt size survives)
- [ ] **Credit total + charge total == 0** for every transfer (test)
- [ ] Both orders reach a terminal status; neither is left permanently `open`
- [ ] A member with no paid seat is reassigned successfully with no orders created
- [ ] Everything is validated before anything is published; money is published before the
      reassignment
- [ ] Replaying the event log twice produces one transfer, not two (deterministic ids;
      test)
- [ ] Provenance on the transfer payments names the original provider payment, root-preserved
- [ ] No lower-bound member-count check on the origin team
- [ ] Contract details (subject, JSON fields, command signature, error set, transfer
      linkage) recorded in the progress log

## Progress Log

<!-- Append entries here — never edit or delete existing entries -->

- 2026-09-07 — Created from hq PRD 012 §6 and §8. Written to be lifted into shared-go.
  Depends on tasks 156, 157, 158. The largest of the four; the roster half and the money
  half are kept together because only a single owner can enforce the publish ordering.
