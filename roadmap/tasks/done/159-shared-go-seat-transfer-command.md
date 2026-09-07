# 159 — [shared-go] Seat transfer command + pre-race member reassignment

**Status:** done
**Priority:** high
**Created:** 2026-09-07
**Picked up by:** zed-agent
**Started:** 2026-09-07
**Completed:** 2026-09-07

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

- [x] A new pre-race reassignment event exists, distinct from `spejder.*.team.moved`, with
      `fromTeamId` / `toTeamId` / actor
- [x] `tables/spejder/consumer.go` consumes it and updates `teamId`, scoped by **year and
      memberId**
- [x] No other event can change `teamId`; the `spejder.*.updated` behaviour is unchanged
- [x] The status projection ignores the new event; `initialTeamId` is never written by it
- [x] One command performs the whole transfer: find lines, create both orders, cover both
      with internal-transfer payments carrying provenance, publish the reassignment last
- [x] Transferable lines include **merchandise as well as participation**, at the price
      snapshotted on the original line, preserving `attributes` (t-shirt size survives)
- [x] **Credit total + charge total == 0** for every transfer (test)
- [x] Both orders reach a terminal status; neither is left permanently `open`
- [x] A member with no paid seat is reassigned successfully with no orders created
- [x] Everything is validated before anything is published; money is published before the
      reassignment
- [x] Replaying the event log twice produces one transfer, not two (deterministic ids;
      test)
- [x] Provenance on the transfer payments names the original provider payment, root-preserved
- [x] No lower-bound member-count check on the origin team
- [x] Contract details (subject, JSON fields, command signature, error set, transfer
      linkage) recorded in the progress log

## Progress Log

<!-- Append entries here — never edit or delete existing entries -->

- 2026-09-07 — Created from hq PRD 012 §6 and §8. Written to be lifted into shared-go.
  Depends on tasks 156, 157, 158. The largest of the four; the roster half and the money
  half are kept together because only a single owner can enforce the publish ordering.
- 2026-09-07 — Lifted into shared-go and picked up, with 156, 157 and 158 landed first.
- 2026-09-07 — Kept as **one** task and one command, for the reason the task gives: the
  publish ordering can only be enforced by whoever owns both publishes.
- 2026-09-07 — **Contract for hq.**

  *Reassignment event.* Subject `NATHEJK.{year}.spejder.{memberId}.reassigned`, body
  `messages.NathejkMemberReassigned` with JSON fields `memberId`, `fromTeamId`, `toTeamId`,
  `actor` (`actor` being the usual `NathejkMemberActor`: `userId`, `name`). It deliberately
  does **not** implement `messages.NathejkMemberEvent` — no `Status()` method — so the
  status projection cannot pick it up and `initialTeamId` is never written by it. A test in
  `messages/member_test.go` guards that.

  *Command.* New package `github.com/nathejk/shared-go/tables/transfer`:

  ```go
  transfer.New(p cqrs.Publisher, roster spejder.RosterReader, lines order.MemberLineReader,
      sources payment.SourceResolver, year types.YearSlug) transfer.Commands

  TransferMember(ctx context.Context, t transfer.Transfer) (*transfer.Result, error)

  type Transfer struct {
      MemberID types.MemberID
      ToTeamID types.TeamID
      TeamType types.TeamType
      Actor    messages.NathejkMemberActor
  }
  ```

  **There is no `FromTeamID` input.** The origin is read from the roster, which is the only
  thing that actually knows it, so a stale form cannot ask for a move out of a team the
  member has already left — and there is no mismatch error for hq to handle.

  *Errors, all distinguishable:*
  - `transfer.ErrIncompleteTransfer` — missing member, destination or team type (a call-site
    bug, not something an operator did).
  - `transfer.ErrMemberNotFound` — the roster has no such member this season. Wraps
    `tables.ErrRecordNotFound`, so existing 404 mapping keeps working.
  - `transfer.ErrSameTeam` — already on that team. **Never** returned for "nothing to
    transfer", which is not an error.
  - `transfer.ErrNotBalanced` — unreachable backstop; a pair that would not net to zero.
  - read errors from the roster / order side pass through unwrapped.

  *No paid seat:* the reassignment succeeds, **no orders are created**, and
  `Result.LineCount == 0` with `Result.Amount == 0`. hq can say so in the UI from the
  result alone.

  *Transfer linkage.* `Result` embeds `transfer.Identity`: `TransferID`, `CreditOrderID`,
  `ChargeOrderID`, `CreditReference`, `ChargeReference`. Every name is a deterministic
  function of `(year, memberId, fromTeamId, toTeamId)` — `transfer.IdentityOf(...)` is
  exported, so hq can recompute them without storing a mapping. Order ids are UUIDv5 (they
  look like every other order id); payment references are `T-{transferId}-C` (credit) and
  `T-{transferId}-D` (charge), hyphenated so they cannot collide with a provider reference,
  which is twelve characters of Crockford base32 and nothing else. **Every line id on both
  halves is `transfer:{transferId}:{n}`**, with the same `n` on both sides — so either
  order identifies its transfer, the pair is findable with one prefix match (no new column),
  and credit line *n* pairs with charge line *n* without matching on amounts.
- 2026-09-07 — Implementation notes.
  - `tables/spejder/consumer.go` gains the `reassigned` subject and a branch that updates
    `teamId`, **scoped by year and memberId** (the file's existing `UPDATE … WHERE memberId`
    is not, which would have rewritten the same member's row in every season). It follows
    the surrounding `fmt.Sprintf`/`%q` style and the file's lowercase `Match` convention;
    the `NATHEJK.`/`NATHEJK:` inconsistency was left alone as instructed. It sets an
    absolute value, so a replay lands on identical row content.
  - Three new narrow read interfaces, each declared by the package that owns the data, so
    nothing was added to an existing `Queries` interface that other repos implement with
    fakes: `spejder.RosterReader` (`TeamIDOf`), `order.MemberLineReader`
    (`PaidLinesByMember`, returning `order.MemberLine` = a line plus its order id), and
    `payment.SourceResolver` gained `SourceOfOrder` (the earliest `reserved`/`received`
    payment on an order, then 158's root resolution).
  - `PaidLinesByMember` returns **every** line for the member on the owner's **paid**
    orders — participation and merchandise — at the `unitPrice` snapshotted on the line,
    ordered by `(orderId, lineId)` so derived ids are stable. Existing credit lines from a
    size change are copied as they are: they come in zero-sum pairs, so the money and the
    shipping answer both survive.
  - Transfer lines are `manual`, not `derived`. A derived line is replaced wholesale by
    `SetDerivedLines`, so a transfer line recorded as derived would vanish the next time the
    destination's order was saved.
  - `EnsureOpenOrder` is not used: both orders are dedicated, exactly as the task requires.
  - **Publish order:** credit order created → credit lines → credit payment
    (`requested` then `received`, negative amount) → charge order → charge lines → charge
    payment (positive) → **reassignment last**. Everything — including the provenance
    lookup — happens before the first publish.
  - Both payments carry `types.PaymentMethodInternalTransfer` and 158's provenance,
    resolved from the paid order contributing the most value (lowest order id breaks a tie,
    so the choice is deterministic).
  - Settlement is left to the payment saga, which 156 taught to settle a credit: both halves
    end with `paidAmount == totalAmount`. The one exception is a transfer whose lines are all
    zero-priced — no payment would be meaningful and the saga only settles orders that owe
    something — so those two halves get `order.paid` with `paidAmount: 0` directly, which is
    exactly what `Commands.Settle` publishes, minus its read-back race against a projection
    that has not yet seen the order created moments earlier.
- 2026-09-07 — Idempotency: every id is derived, so re-running the command re-states the
  same transfer (the projectors upsert by `(orderId, lineId)` and `handlePaid` still guards
  on `status='open'`) instead of crediting anybody twice. The deliberate trade-off, recorded
  here: moving the same member A → B twice with a move back in between reuses the first
  transfer's names and collapses into one transfer of the same value — a far better failure
  mode than a double credit.
- 2026-09-07 — Checked the claim about other consumers. `spejder.teamId` becoming mutable is
  reached only by the new subject in a new branch. tilmelding reads the roster through
  `Spejder.GetAll(Filter{TeamID: …})`; nothing there assumes the value never changes — a
  reassigned member simply stops appearing in the origin's roster, which is the intended
  pre-race meaning. Its derived-lines path then bills the origin for one seat fewer, and
  because the credit sits on a **paid** order the origin's paid-unit count drops by the same
  seat, so nothing is re-charged on either side.
- 2026-09-07 — Tests: `tables/transfer/transfer_test.go` (nets to zero; merchandise moves
  with its size, snapshotted price, memberId and manual origin; money published before the
  reassignment and exactly one reassignment; no paid seat → reassignment only and no
  provenance lookup; determinism across two runs including line ids, and a different
  destination being a different transfer; both halves covered by internal-transfer payments
  with `requested` before `received`, signed amounts, order linkage and provenance;
  provenance taken from the funding order; zero-value halves settled without a payment;
  every refusal publishing nothing; read errors surfacing; the balance backstop; line ids
  naming the transfer and pairing the halves; identity names distinct and year-scoped),
  `tables/spejder/consumer_test.go` (teamId updated, scoped by year and memberId; the
  subject subscribed; `spejder.updated` still not touching teamId; an empty destination
  ignored), and `messages/member_test.go` (the reassignment is not a lifecycle event, while
  team.moved still is). `go test ./...`, `go vet ./...` and `gofmt -l .` all clean.
- 2026-09-07 — Completed. All four tasks (156–159) are done in shared-go; nothing is wired
  into a service yet — mounting `transfer.New` in a composition root is hq's side of PRD 012.
