# 168 — [shared-go] Transfer orders should not depend on the payment saga winning a race

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

A seat transfer creates two orders — a **credit** on the sending team and a **charge** on the
receiving team — and pays both with internal-transfer payments it publishes itself. Closing
those orders is then left to the payment saga, which reacts to `payment.received`.

**The credit order frequently never closes.** Observed repeatedly in dev: three transfers,
two of them left the credit order `open` forever while the charge order settled. What an
operator sees on the sending team's payment list is self-contradictory:

| Tidspunkt | Beløb | Betalt | Mangler | Status |
|---|---|---|---|---|
| 7. sep. 22.42 | −425,00 kr. | −425,00 kr. | 0,00 kr. | **Åben** |

Nothing is owed and it still says open.

### Why it happens

The saga's evaluation is a **one-shot reaction** to `payment.received` that reads the
service's *own* projections. But the projector and the saga are separate consumers with no
ordering between them, and the transfer publishes its events in a burst:

```
order.{credit}.created
order.{credit}.lines.changed
payment.{credit}.requested
payment.{credit}.received     <- saga reacts here, ~30ms after the order was created
order.{charge}.created
...
payment.{charge}.received     <- by now the projector has caught up
```

So when the saga evaluates the credit order, its own `orders` / `payment` rows may not exist
yet. It retries — 5 attempts over about 2 seconds — and then **gives up permanently and
silently**. The order is never revisited, because nothing else will ever publish another
`payment.received` for it.

**The credit side loses this race far more often, and structurally so:** it is published
first, so its payment arrives when the projector has had the least time to catch up. The
charge order gets four more messages' worth of head start.

Proof that the data is fine and only the decision was lost — read from the saga's own
database *after* the fact:

```
orders:   6e0a06f6…  status=open   totalAmount=-42500
payment:  T-G7SNB0XKY8WW-C  amount=-42500  status=received  orderForeignKey=6e0a06f6…
```

Fully covered, still open. Restarting the service replays the stream, the saga re-evaluates
with everything projected, and all the orders settle — which is the current, undocumented
recovery procedure and not an acceptable one.

## Notes

### The fix worth making: let the transfer close its own orders

The transfer command **already knows** both orders are fully covered — it created the orders
*and* the payments in the same operation. Discovering that fact again, asynchronously, via
another service's projection lag, is the design flaw.

There is already a precedent for this **in the same command**: a transfer whose lines are all
zero-priced publishes `order.paid` directly, because the saga only settles orders that owe
something. Extending that to *every* transfer order is a small, consistent change:

- It removes the race entirely; a transfer is atomic in effect as well as in intent.
- It removes the dependency on any other service being alive or current for a transfer to
  complete — the credit and charge halves become as reliable as the reassignment itself.
- **It also collapses the window in hq task 166**, where a second transfer of the same member
  moves no money because the previous charge order is not yet `paid`. That window exists only
  because settlement is deferred. Closing it here makes hq's refusal a formality rather than
  a routine occurrence.

Care needed: the saga must remain idempotent when it later sees the same
`payment.received` — the existing `AND status='open'` guard on the projector's `handlePaid`
covers the projection, and the saga's own `if o.Status != StatusOpen { return settled }` check
covers the publish. Verify both, because this deliberately creates the case where the order is
*already* paid when the saga arrives.

### The second fix: a silent permanent give-up is the real sin

Independently of the above, the saga abandoning an order forever with **no log line** is what
made this expensive to find. There is a log for the not-yet-projected case
(`order still not projected after 5 attempts; will settle on a later replay`) but none for the
"looks underpaid" exhaustion, which is the branch that actually fired here. Any exhaustion
should say so, name the order and payment, and state that it needs a replay — otherwise the
next occurrence is invisible again.

Worth reconsidering too: **should the saga re-evaluate on `order.lines.changed`?** An order
that gains its total *after* its payments arrived is exactly the shape that loses this race,
and reacting to the order side as well as the money side would make the saga self-healing
without a restart. Optional if the transfer publishes its own `order.paid`, valuable if not.

### What not to do

- **Do not widen the retry budget and call it fixed.** It is a race with no upper bound on the
  other side; a bigger number moves the failure rather than removing it, and hides it better.
- **Do not let a caller mark an order paid without covering it.** The point is that the
  transfer *has* covered it, not that paid is a state anyone may assert.
- **Do not paper over it in the UI** by rendering `dueAmount <= 0` as *Betalt*. The order
  genuinely is open and genuinely is mutable; a display that hides that would leave a
  rewritable settled exchange looking finished.

## Acceptance Criteria

- [x] A transfer's credit and charge orders both reach a terminal state as part of the
      transfer, without depending on another service's projection timing
- [~] Verified by performing transfers repeatedly in quick succession — the failure is a race,
      so a single passing run proves nothing *(partially: the race is removed structurally
      rather than made less likely, and the unit tests pin it, but a dev-loop run of repeated
      transfers was not performed here — see the progress log)*
- [x] Replay-safe: a full replay still produces one settled pair, and the saga seeing an
      already-paid transfer order is a no-op
- [x] Any saga give-up logs the order, the payment and the fact that it needs a replay
- [x] Decided and recorded: whether the saga also re-evaluates on `order.lines.changed`
- [x] hq task 166's settlement window is confirmed closed (or explicitly still open, with why)

## Progress Log

<!-- Append entries here — never edit or delete existing entries -->

- 2026-09-07 — Lifted into shared-go and picked up. Reviewed: the diagnosis is correct and
  this is a regression introduced by task 159, which chose to let the saga settle both halves
  precisely because 156 had taught it to settle a credit. That reasoning was sound for
  *money arriving from outside* and wrong here: the transfer publishes the payment itself, so
  the saga is being asked to rediscover, across a consumer boundary, something the publisher
  already knew. 159's own progress log even argued *against* route B on the grounds that a
  freshly published order cannot be read back reliably — and then depended on exactly that
  read happening in another consumer.
- 2026-09-07 — **Fix 1: the transfer closes its own orders.** `publishHalf` in
  `tables/transfer/transfer.go` now publishes `order.paid` for every half, with the order's
  own signed `paidAmount` (negative for the credit), after the payment events so the money
  trail always precedes the state change. The zero-priced special case did not need to stay
  special: it collapsed into the general path, which is what the task suggested. Sequence per
  transfer is now created → lines.changed → payment.requested → payment.received →
  order.paid, credit half then charge half, reassignment last; a test pins all eleven
  subjects in order.

  This removes the race rather than widening the budget: there is no longer any cross-consumer
  dependency in a transfer's settlement, so it no longer matters how far behind the order
  projector is, or whether the saga's host service is even running.
- 2026-09-07 — Idempotency verified on both layers, since this deliberately makes
  "already paid when the saga arrives" the normal case: the projector's `handlePaid` still
  guards `WHERE status='open'`, and `attemptTransition` still returns early for any order that
  is not open. New tests assert the saga publishes nothing, retries nothing and logs nothing
  for an already-settled transfer order — both the charge and the credit half.
- 2026-09-07 — **Fix 2: no more silent give-up.** Both exhaustion branches in
  `tables/order/saga.go` now log, and both name the payment, name the order and say that a
  replay is what settles it. Previously only the not-yet-projected branch logged, and the
  branch that actually fired in this incident — "looks under-paid" — said nothing, which made
  a give-up indistinguishable from an order correctly left alone.

  To name the order, `attemptTransition` now returns an `attempt{result, orderID}` instead of
  a bare `attemptResult`; the order id is filled in as soon as the payment names one. An
  unnamed order renders as `(unknown)` rather than an empty gap.
- 2026-09-07 — **Decision: the saga does not re-evaluate on `order.lines.changed`.** Rejected,
  and not just as unnecessary now that transfers settle themselves. It would change behaviour
  for the ordinary flow in a harmful way: an order whose total legitimately drops below what
  has already been paid would settle, and settling means immutable. A team that pays for five
  seats and then removes a member would find its own order frozen and be unable to add anybody
  back — `SetDerivedLines` refuses a paid order with `ErrNotOpen`. Trading a stuck-open order
  for a stuck-closed one, on the ordinary path rather than the transfer path, is the wrong
  trade. The saga stays money-triggered; the self-healing the task wanted is achieved by the
  publisher closing what it covered.
- 2026-09-07 — **hq task 166's window: closed as a routine occurrence, not as a theoretical
  one.** A second transfer of the same member no longer waits on the saga (a ~2s retry budget
  that could end in permanent abandonment) but only on the order projector consuming an
  `order.paid` published in the same burst — ordinary millisecond-scale lag on the same
  consumer that had to project the order in the first place. hq's refusal becomes a formality.
  It is not provably zero, because hq's check reads a projection and projections are
  eventually consistent; keep the refusal, and it is now safe to word it as "prøv igen" rather
  than as a condition that needs an operator to understand it.
- 2026-09-07 — **On verification, honestly.** `go test ./...`, `go vet ./...` and
  `gofmt -l .` are clean, and `go test -race -count=20 ./tables/transfer/ ./tables/order/`
  passes — but that is not the same as the task's "perform transfers repeatedly in quick
  succession", which needs the dev stack and was not run here. What can be said is stronger
  than a passing run anyway: the failure mode was a dependency on another consumer's progress,
  and that dependency no longer exists, so there is no window left to lose. The one thing a
  dev-loop run would still add is confirmation that nothing *else* in the burst is racy;
  worth doing on the hq side when the endpoint is wired.
- 2026-09-07 — `docs/moving-a-paid-member.md` updated, since it doubles as the report to hq:
  the published-event list now shows both `order.paid` events, §6 explains why the command
  closes its own orders and what that means for a caller (both halves appear PAID at once,
  the credit total is negative), and §2 no longer claims a transfer needs the saga mounted —
  it still is, for ordinary MobilePay payments.
- 2026-09-07 — Completed. Regression fixed at its root; the saga is now honest when it gives
  up, which is what would have made this cheap to find in the first place.
- 2026-09-07 — Created after an operator saw a credit order showing **Åben** with
  **Mangler 0,00 kr.** on a sending team. Diagnosed to the saga losing a race against its own
  projections and abandoning the order silently; confirmed from the saga's own database that
  the order was fully covered and still open, and that restarting the service settled every
  stuck order. Three transfers observed, two credit orders stuck — this is the common case,
  not an edge one.
