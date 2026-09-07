# Moving a paid member between teams

How a downstream service uses the seat-transfer capability added by shared-go
tasks 156–159. Written for whoever wires and calls it (hq today); everything a
caller needs is here, and nothing in it requires reading the tasks.

Nothing in shared-go mounts or calls any of this — the module has no `main`. A
service must wire the command and expose it, which is what the rest of this
document describes.

---

## 1. What the capability is

One command moves a member to another team **before the race**, and moves the
money paid for them along with them:

- the member's `teamId` changes on the `spejder` roster (this is new — until now
  no event in the system could change it);
- every line the origin had paid for on that member's behalf — the participation
  seat **and** any merchandise — is credited back to the origin and charged to the
  destination, at the price snapshotted on the original line;
- both sides are covered by **internal-transfer** payments carrying provenance, so
  the original MobilePay payment that brought the money in is still nameable;
- both orders are closed by the command itself, so a transfer completes without
  depending on another consumer's timing;
- the pair nets to zero. Always. That is the invariant the whole thing rests on.

It is a pre-race reassignment, which is a different fact from the race-time
`spejder.*.team.moved`. See §7.

---

## 2. Wiring

```go
import (
    "github.com/nathejk/shared-go/tables/order"
    "github.com/nathejk/shared-go/tables/payment"
    "github.com/nathejk/shared-go/tables/spejder"
    "github.com/nathejk/shared-go/tables/transfer"
)

roster   := spejder.New(w, r)
orders   := order.New(p, w, r, year, products)
payments := payment.New(p, w, r, year, payment.WithProvider(mobilepay))

transfers := transfer.New(p, roster, orders, payments, year)
```

The three collaborators are narrow read interfaces, each declared by the package
that owns the data, and each already satisfied by that package's entity:

| Parameter | Interface | Supplies |
|---|---|---|
| `roster` | `spejder.RosterReader` | which team the member is on |
| `lines` | `order.MemberLineReader` | what has been paid for them |
| `sources` | `payment.SourceResolver` | where that money originally came from |

Nothing new goes on the consumer mux: the command only publishes. The projections
that consume what it publishes — `order`, `payment`, `spejder` — must all be
mounted, which they already are.

A transfer does **not** need the payment saga: it closes its own two orders (§6).
The saga must still be mounted in exactly one service — it is, in `tilmelding` —
for ordinary MobilePay payments. Do not add a second mount.

---

## 3. Calling it

```go
res, err := transfers.TransferMember(ctx, transfer.Transfer{
    MemberID: memberID,
    ToTeamID: destinationTeamID,
    TeamType: types.TeamTypePatrulje,
    Actor:    messages.NathejkMemberActor{UserID: user.ID, Name: user.Name},
})
```

**There is no `FromTeamID`.** The origin is read from the roster, which is the
only thing that actually knows it, so a stale form cannot ask for a move out of a
team the member has already left — and there is no mismatch error to handle.

`TeamType` is the kind of team on both sides; a member moves between teams of the
same kind. It is what makes the origin's orders findable, since orders are keyed
by `(year, ownerType, ownerId)`.

### The result

```go
type Result struct {
    transfer.Identity      // TransferID, CreditOrderID, ChargeOrderID,
                           // CreditReference, ChargeReference
    FromTeamID types.TeamID
    Amount     int          // value moved, minor units, always >= 0
    LineCount  int          // 0 means the member had no paid seat
}
```

Read the outcome off this rather than re-reading a projection: the events have
only just been published, so a read-back will usually be behind.

`LineCount == 0` is an **ordinary success**, not a failure — see §5.

---

## 4. Errors

Each is its own value because each deserves its own message to an operator.

| Error | Meaning | Suggested handling |
|---|---|---|
| `transfer.ErrIncompleteTransfer` | missing member, destination or team type | 500 — a call-site bug, not the operator |
| `transfer.ErrMemberNotFound` | the roster has no such member this season | 404 (it wraps `tables.ErrRecordNotFound`, so existing mapping works) |
| `transfer.ErrSameTeam` | already on that team | 422, e.g. *"spejderen er allerede på den patrulje"* |
| `transfer.ErrNotBalanced` | the pair would not net to zero | 500 — unreachable backstop; if it ever fires, something is badly wrong and nothing was published |
| anything else | a failed read | 500 |

Compare with `errors.Is`.

**"Already on that team" and "nothing to transfer" are deliberately not the same
thing.** The first is a refusal; the second is a success with no orders.

### Preconditions are the caller's job

The command refuses only what it alone can know: a member who does not exist, and
a move to the team the member is already on. Everything else belongs to the
service that owns the team read models and the operator-facing wording:

- the destination has accepted;
- the destination has not started;
- the destination is under its member cap.

And one thing the command must **not** be asked to enforce: there is no lower
bound on the origin's member count, and none should be added. Emptying a team out
one member at a time is a required capability — a team below three may not start,
and its remaining members have to be transferable to a team that has not started.

---

## 5. What happens when nothing has been paid

The reassignment still happens, **no orders are created**, and the result says so
(`LineCount == 0`, `Amount == 0`). This is the normal case for a member who was
added to a roster but whose team has not paid yet.

Surface it: an operator who sees "flyttet" with no money movement should be told
that there was nothing to move, rather than left wondering.

---

## 6. What it publishes, and in what order

```
NATHEJK.{year}.order.{creditOrderId}.created
NATHEJK.{year}.order.{creditOrderId}.lines.changed        (negated lines)
NATHEJK.{year}.payment.{creditReference}.requested        (negative amount)
NATHEJK.{year}.payment.{creditReference}.received         (negative amount)
NATHEJK.{year}.order.{creditOrderId}.paid                 (negative paidAmount)
NATHEJK.{year}.order.{chargeOrderId}.created
NATHEJK.{year}.order.{chargeOrderId}.lines.changed        (positive lines)
NATHEJK.{year}.payment.{chargeReference}.requested        (positive amount)
NATHEJK.{year}.payment.{chargeReference}.received         (positive amount)
NATHEJK.{year}.order.{chargeOrderId}.paid                 (positive paidAmount)
NATHEJK.{year}.spejder.{memberId}.reassigned              (last)
```

Everything is validated — including the provenance lookup — before the first
publish.

**Money first, reassignment last, on purpose.** There is no transaction across
JetStream, so a partial failure is possible and the ordering picks which one you
get. Money moved and member not is invisible: funds have shifted for somebody who
is still on the old roster and nothing looks wrong. Member moved and money not is
a member on a team whose seat was never paid for, which shows up in the numbers
the moment anyone looks. Between an invisible inconsistency and a visible one,
take the visible one.

If `TransferMember` returns an error, assume a partial publish is possible and
report the failure rather than retrying blindly — although a retry is safe (§8).

### How the two orders reach a terminal state

**The command closes them itself**, in the same burst — see the two `order.paid`
events above. A transfer therefore does not depend on any other consumer, or any
other service, being alive or current.

This was not the first design, and the history is worth knowing because it explains
the shape. Settlement was originally left to the **payment saga**, which reacts to
`payment.received` and reads its own projections. But this command publishes in a
burst: the credit order's payment lands milliseconds after the order was created,
before the order projector has necessarily written it. The saga would then evaluate
an order it could not see, retry for about two seconds, and give up **permanently**
— nothing ever publishes another `payment.received` for that order. The credit half
lost that race structurally, being published first with the least head start. In
dev, two of three transfers left the sending team's credit order showing *Åben*
with *Mangler 0,00 kr.*, recoverable only by restarting the service so the stream
replayed.

The command already knows both orders are covered — it created the orders *and* the
payments. Rediscovering that asynchronously through another consumer's lag was the
flaw; widening the retry budget would only have hidden it, since the window has no
upper bound.

This is not "a caller may assert paid". It is the same freedom
`order.Commands.Settle` has, for the same reason: the order owes nothing, and here
that is *known* rather than believed. The saga remains the only path for an order
whose money arrives from outside, and it stays idempotent — an already-paid order
is an early return there, and the projector's `handlePaid` guards on
`status='open'`.

Consequences for the caller:

- **Both orders appear as `PAID`** essentially immediately, subject only to
  ordinary projection lag. Any UI that sums or renders order totals should expect
  the credit order's negative total. `order.Status` on the wire is unchanged
  (`"OPEN"` / `"PAID"`).
- A transfer's settlement no longer requires the saga's host service to be running.
  The saga must still be mounted for *ordinary* MobilePay payments.

---

## 7. The reassignment event

```
subject: NATHEJK.{year}.spejder.{memberId}.reassigned
body:    messages.NathejkMemberReassigned
         { memberId, fromTeamId, toTeamId, actor{userId,name} }
```

Projected by the `spejder` entity, which updates `teamId` scoped by year **and**
member. It is the only event that writes `teamId`; `spejder.*.updated`
deliberately still leaves it alone.

**It is not a lifecycle event and must not be treated as one.** It has no
`Status()` method, so it does not satisfy `messages.NathejkMemberEvent`, and a
status projection dispatching on that interface will not see it. That is
deliberate and a test guards it:

- pre-race there is no status row and no `activeMemberCount` to recompute;
- writing `initialTeamId` would be actively wrong — a reassigned member who later
  races has **always** been on their new team.

Contrast `spejder.*.team.moved` (`NathejkMemberTeamMoved`), which is a race-time
fact: somebody who *started* with one patrol and continued with another. There the
origin keeps them on its roster and `initialTeamId` is preserved. Do not reuse
either event for the other's purpose — doing so would make `initialTeamId`, the
record of where somebody started, into a lie.

---

## 8. Identity, idempotency and finding a transfer again

Every name a transfer publishes is a deterministic function of
`(year, memberId, fromTeamId, toTeamId)`, and the derivation is exported:

```go
id := transfer.IdentityOf(year, memberID, fromTeamID, toTeamID)
// id.TransferID, id.CreditOrderID, id.ChargeOrderID,
// id.CreditReference, id.ChargeReference
// id.LineID(n)
```

- **Order ids** are UUIDv5, so they look like every other order id.
- **Payment references** are `T-{transferId}-C` (credit) and `T-{transferId}-D`
  (charge). Hyphenated so they cannot be confused with a provider reference, which
  is twelve characters of Crockford base32 and nothing else.
- **Line ids** are `transfer:{transferId}:{n}`, with the same `n` on both halves.

Three things follow, all of them useful:

1. **A repeated call is harmless.** A double-submitted form re-states the same
   transfer — the projectors upsert by `(orderId, lineId)`, and `handlePaid` still
   guards on `status='open'` — instead of crediting anybody twice.
2. **A replay produces one transfer, not two.**
3. **The pair is findable without a new column.** Either order's lines name the
   transfer, so `lineId LIKE 'transfer:{id}:%'` finds both halves, and credit line
   *n* pairs with charge line *n* without matching on amounts. You can also just
   recompute both order ids with `IdentityOf`.

The trade-off, stated plainly: moving the same member A → B twice, with a move
back to A in between, reuses the first transfer's names and collapses into a
single transfer of the same value. That is a far better failure mode than a double
credit, but it means the ids identify *a move between two teams*, not *an
occasion*.

---

## 9. Provenance: answering "who paid for this?"

Every internal-transfer payment carries where its money originally came from.

```go
type PaymentSource struct {   // types.PaymentSource
    Reference string          // the ROOT provider payment, or "unknown"
    Via       string          // immediate predecessor, if the money moved before
    OwnerType types.TeamType
    OwnerID   string
    Method    types.PaymentMethod
    PaidAt    string
}
```

On the wire it is `messages.NathejkPaymentRequested.Source`
(`json:"source,omitempty"`), and in the read model it is
`payment.Payment.SourceReference` (indexed) plus `payment.Payment.Source` (the
full snapshot).

**Three states, and they must stay distinguishable:**

| State | Event | Row |
|---|---|---|
| not a transfer | `Source == nil` | `sourceReference = ''`, `source = '{}'` |
| known | `Source.Known()` | `sourceReference =` the root's reference |
| unknown | `Source.Reference == types.PaymentSourceUnknown` | `sourceReference = 'unknown'` |

A provider payment has no provenance because it *is* the provenance. "Unknown" is
information — *we looked and could not tell* — and it is common rather than
exceptional: most historical payments are not reachable from an order at all. It
never blocks a transfer.

`Reference` is the **root**, not one hop back: money can move A → B → C and the
answer stays A, with B in `Via`.

### Rendering it

There is deliberately **no owner name** on the source, only `ownerType` +
`ownerId`. Names live in the team read models, which shared-go has no business
reading and which a caller can join for a *current* name; the id is the part that
stays true. So compose the display yourself:

> betalt af Patrulje 12 (MobilePay, 4. juni)

from `ownerId` (→ your team read model), `method` and `paidAt`. Fall back to
something honest when the source is unknown — *"oprindelig betaling ukendt"* —
rather than hiding the transfer.

For a lookup in the other direction ("every transfer funded by payment X"), query
`payment.sourceReference`; it is indexed as `idx_payment_source`.

---

## 10. Payment methods

`payment.method` is now typed:

```go
types.PaymentMethodMobilePay        // "mobilepay"        — unchanged, every existing row
types.PaymentMethodInternalTransfer // "internal-transfer"
types.PaymentMethodNone             // ""                 — legacy / unset, not publishable
```

Compare against the constants rather than literals. `Valid()` excludes the empty
value.

An internal transfer has **no provider behind it** and must never be handed to
one: there is no transaction to create, authorise or capture, and doing so would
take money a second time. It is also not a refund — nothing leaves the system.

---

## 11. Upgrade notes for a consuming service

Pull the module and expect these:

1. **One source-compatibility break.** `payment.Payment.Method` is now
   `types.PaymentMethod` instead of `string` (matching `Status` in the same
   struct). Code assigning it to a `string` needs `string(p.Method)`. Nothing
   changes on the wire, in the schema or at runtime.
2. **Two new `payment` columns plus an index**, added through `cqrs.EnsureColumn` /
   `cqrs.EnsureIndex` in `payment.New` — so an existing database gets them on the
   next start, not just a freshly created one. No manual migration.
3. **`spejder.teamId` is now mutable**, but only via the new `reassigned` subject
   in a new branch. Nothing that works today behaves differently. Confirmed
   against `tilmelding`, which reads the roster by team filter and does not assume
   immutability: a reassigned member simply stops appearing in the origin's
   roster, and because the credit sits on a **paid** order the origin's paid-unit
   count drops by the same seat, so nothing is re-charged on either side.
4. **Everything else is additive.** New optional event fields decode to their zero
   values, so events published before these changes replay unchanged; existing
   readers ignore columns they do not select.

Nothing that settles today stops settling. The only orders whose fate changed are
negative-total ones, of which there were none until this command started creating
them.

---

## 12. Checklist

- [ ] `transfer.New(...)` wired in the composition root
- [ ] the payment saga is mounted in exactly one service (still `tilmelding`) — for
      ordinary payments; a transfer no longer depends on it
- [ ] endpoint enforces its own preconditions: accepted, not started, under the cap
- [ ] **no** minimum-member check on the origin
- [ ] each error mapped to its own message; `ErrSameTeam` distinct from
      `LineCount == 0`
- [ ] UI tolerates a negative order total and shows both halves as paid
- [ ] provenance rendered, with an honest fallback for `unknown`
- [ ] `string(p.Method)` fixed up where the read model's method was used as a string
