# 156 — [shared-go] Settle credit (negative-total) orders

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

## Description

An order whose `TotalAmount` is **negative** can never reach a terminal state. It stays
`open` forever, which means it stays **mutable** forever — and a later `SetDerivedLines`
would silently rewrite a credit that has already been acted on. Nothing errors and
nothing is logged, so the order simply sits there looking unpaid.

Both exits from `open` refuse it:

`tables/order/saga.go` (the Pay saga, reacting to `payment.received`):

```go
	if o.Status != StatusOpen {
		return resultSettled, nil
	}
	// A free order (TotalAmount == 0) shouldn't auto-transition on a random
	// payment hitting it — it'd never be in this code path without a positive
	// payment, but guard anyway.
	if o.TotalAmount <= 0 {
		return resultSettled, nil
	}
	if o.PaidAmount < o.TotalAmount {
		return resultUnderpaid, nil
	}
```

`tables/order/commander.go` — `Settle` refuses anything whose total is not exactly zero:

```go
// ErrOrderNotFree is returned by Settle for an order whose total is not zero.
var ErrOrderNotFree = errors.New("order total is not zero")
...
		return nil, fmt.Errorf("%d: %w", o.TotalAmount, ErrOrderNotFree)
```

So `TotalAmount < 0` falls between the two: the saga treats it as "nothing to do here"
and `Settle` treats it as "you owe money". Note the `<= 0` guard is documented as being
about the **zero** case; the negative case was not considered when it was written.

### Why this matters on its own terms

A negative total is already a legitimate, documented shape in this package. From
`tables/order/commander.go`:

> Quantity is normally positive. A negative quantity is a credit: it reclaims a unit
> already paid for, and is produced only by `ApplyPaidOffset`, always paired with a
> positive line for the same SKU so the pair costs nothing.

`ApplyPaidOffset` only ever produces credits *paired inside one order*, so today every
total stays >= 0 and the gap is unreachable. But `order_line.quantity`, `lineTotal` and
`orders.totalAmount` are all plain signed `INT`, and `SetDerivedLines` / `AddManualLine`
validate eligibility, active-ness, stock and `memberId` — **never the sign of the
total**. The moment a credit is issued to one owner and the matching charge to another
(the case PRD 012 needs, but equally any refund, goodwill credit or correction), the
credit order becomes permanently unclosable.

The invariant worth restoring is simple: **an order that owes nothing is settleable, and
"owes nothing" includes "is owed".** A credit fully covered by a matching negative
payment is as settled as a charge fully covered by a positive one.

### What to decide and implement

Two viable routes. Pick one, record the reason in the progress log:

- **A — teach the saga.** Generalise the guard so a credit order settles when its
  `PaidAmount` has reached its (negative) `TotalAmount`, i.e. compare "outstanding"
  rather than assuming positive amounts: settle when `TotalAmount != 0` and
  `PaidAmount <= TotalAmount` for negatives / `>= TotalAmount` for positives. Keeps one
  settlement path for every order. Requires the saga to be triggered for the credit side,
  which means a negative `payment.received` event must exist (see task 159 and the
  symmetry decision below).
- **B — generalise `Settle`.** Redefine it from "freezes an order that costs nothing" to
  "freezes an order that owes nothing", accepting a credit order whose payments cover it,
  and keep the saga strictly about money arriving from outside. The doc comment's own
  framing — *"Settle is how a caller says 'this is what was agreed' without money changing
  hands"* — fits an internal credit well.

**Related decision, made by whoever picks this up first:** whether a credit order is
covered by a **negative payment** (symmetrical: `paidAmount == totalAmount` on both
halves of a transfer, both auditable the same way) or by **no payment at all** (keeps
`payment.amount` never-negative, but leaves the credit side with no payment trail). Route
A effectively requires the negative payment; route B works with either. Note that
`payment.amount` is a signed `INT` and the paid-amount subquery just sums it:

```go
COALESCE((SELECT SUM(p.amount) FROM payment p WHERE p.orderForeignKey = o.orderId AND p.status IN ('reserved', 'received')), 0) AS paidAmount
```

Whichever route is chosen, a negative-total order must **not** be left able to reach
`paid` while still uncovered — a credit nobody has honoured is not settled, and freezing
it would hide that.

## Notes

- **Do not "fix" this by forbidding negative totals.** They are the intended
  representation of a credit in this package, and blocking them would push callers into
  encoding credits as something else.
- `checkStock` already handles credits correctly — it counts only positive quantities
  (`CASE WHEN l.quantity > 0 THEN l.quantity ELSE 0 END` in `querier.go`'s
  `ReservedQuantity`), so a credit does not release seat stock. No change wanted there.
- The saga's existing retry/`resultUnderpaid` machinery distinguishes "not yet paid
  enough" (retry only when live) from "order not projected yet" (retry even during
  replay). A credit order that is not yet covered should reuse `resultUnderpaid`, not
  invent a third outcome.
- The saga is mounted in **exactly one** service (tilmelding). Do not add a second mount
  as part of this; the existing comment in hq's composition root explains that the
  "transition only if still open" check is a read-then-publish with no compare-and-swap.
- **Idempotency:** `handlePaid` in `tables/order/consumer.go` guards its UPDATE with
  `AND status='open'`, so a replayed `order.paid` is harmless. Keep it that way for
  credits.

### Contract that hq will depend on

- `order.Status` on the wire stays `"OPEN"` / `"PAID"` (`Status.MarshalJSON` collapses
  `cancelled` into `"PAID"`). If this task changes that, say so loudly — hq renders the
  binary value directly.
- If a new exported error or a changed `Settle` signature results, name it here in the
  progress log so hq's error mapping can be written against it.

### Impact on other consumers

`orders` / `order_line` / `payment` are read by both tilmelding and hq. This change is
**additive to behaviour**: no order that settles today stops settling, and no order that
is refused today is refused differently — the only orders whose fate changes are those
with a negative total, of which there are currently none in production, because nothing
creates them yet. That is what makes this safe to land before its caller (task 159)
exists.

## Acceptance Criteria

- [x] A credit order (negative `totalAmount`) fully covered by its payment(s) reaches a
      terminal status instead of remaining `open`
- [x] A credit order that is **not** covered stays `open` and is not frozen
- [x] Positive-total orders behave exactly as before — same settlement point, same
      over-payment behaviour
- [x] Zero-total orders still settle only via `Settle`, unchanged
- [x] Replaying the same events twice leaves the order in the same state (no double
      publish of `order.paid`, existing `status='open'` guard preserved)
- [x] Unit test covering: credit covered, credit uncovered, positive covered, zero
- [x] Chosen route (A or B) and the negative-payment-vs-no-payment decision recorded in
      the progress log with the reason
- [x] No new stock behaviour: credits still do not release reserved stock

## Progress Log

<!-- Append entries here — never edit or delete existing entries -->

- 2026-09-07 — Created from hq PRD 012 §8 obstacle 2 and §11 Q1/Q2. Written to be lifted
  into shared-go. Blocks task 159; task 157 and 158 are independent of it.
- 2026-09-07 — Lifted into shared-go and picked up. Plan: route A (teach the saga), with
  the symmetrical negative payment. Reason recorded in the next entry.
- 2026-09-07 — **Decision: route A (teach the saga), covered by a symmetrical negative
  payment.** Reason: route B would require task 159 to call `Settle` on the two orders it
  has just created *by publishing events*, so the read model it reads would almost always
  be behind and the call would fail on `ErrRecordNotFound` or on a stale `TotalAmount`.
  The saga already owns exactly that problem — its `resultUnprojected` / `resultUnderpaid`
  retry budget exists because the payment and order projections lag the events it reacts
  to — so putting credit settlement anywhere else means reinventing it. Keeping one
  settlement path also means there is one place where "this order owes nothing further"
  is decided, in both directions.

  The negative payment follows from route A (the saga is triggered by
  `payment.*.received`), but is the better shape independently: `payment.amount` is already
  a signed `INT` and the `paidAmount` subquery already plain-`SUM`s it, so both halves of a
  transfer end up with `paidAmount == totalAmount` and are auditable identically. "No
  payment at all" would leave the credit side with no trail precisely where somebody will
  ask where the money went.
- 2026-09-07 — Implemented in `tables/order/saga.go`. `attemptTransition`'s
  `TotalAmount <= 0` guard became `TotalAmount == 0` (zero-total orders still reach
  `paid` only through `Commands.Settle`), and the `PaidAmount < TotalAmount` comparison
  became a new package-level `covered(total, paid int) bool`: `paid >= total` for a charge,
  `paid <= total` for a credit. An uncovered credit returns the existing
  `resultUnderpaid`, so it keeps the retry budget and stays `open`. No third outcome, no
  new event, no projector change, no schema change.
- 2026-09-07 — Tests added to `tables/order/saga_test.go`: covered credit settles (and
  publishes its own negative `paidAmount`), uncovered credit stays open with the underpaid
  retry budget (no payment yet, and partially credited), over-credit settles, a table
  pinning the settlement point for all eight total/paid shapes, and a two-delivery replay
  proving one `order.paid`. `go test ./tables/order/` green; full `go test ./...` green.
- 2026-09-07 — **Contract for hq:** nothing changed. `order.Status` on the wire is still
  `"OPEN"` / `"PAID"`, no exported error was added or removed, and `Settle`'s signature is
  untouched. `Settle` still returns `ErrOrderNotFree` for a credit order — by design:
  credits settle through the saga, and its doc comment now says so. The only observable
  difference is that a negative-total order covered by a negative payment now reaches
  `PAID`; there are no such orders in production yet, because nothing creates them until
  task 159.
- 2026-09-07 — Completed. Credit orders are settleable; task 159 is unblocked on this axis.
