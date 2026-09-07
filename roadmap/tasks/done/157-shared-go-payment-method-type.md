# 157 — [shared-go] Typed payment method with an internal-transfer value

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

`payment.method` is a free-text column with exactly one value ever written, hardcoded at
a single call site in `tables/payment/commands.go`:

```go
	body := messages.NathejkPaymentRequested{
		Reference:       resp.Reference,
		...
		Method:          "mobilepay",
		OrderLines:      messageLines(lines),
		OrderForeignKey: ch.OrderID,
		OrderType:       OrderTypeOrder,
	}
```

The column is `method VARCHAR(99) NOT NULL DEFAULT ""` and the message field is
`Method string`. There is **no `PaymentMethod` type in `types/`**, unlike
`types.PaymentStatus`, `types.Currency`, `types.SignupStatus` and the rest, which are all
typed constants.

That was fine while MobilePay was the only way money arrived. It is about to stop being
fine: a second payment method is coming — an **internal transfer**, which moves money
already received between orders without any provider involvement — and a second free
string in a second repo is how two spellings of the same concept end up in the same
column, with every reader guessing.

### What to do

1. **Add `types.PaymentMethod`** alongside `types.PaymentStatus` in `types/payment.go`,
   with constants for the existing `mobilepay` and the new `internal-transfer`. Follow the
   conventions already in that file (string-backed type, `Valid()` if the neighbours have
   one). Keep the wire values lowercase and stable — `mobilepay` **must** keep its exact
   current spelling, because it is already in the event log and in the `payment.method`
   column of every existing row.
2. **Use the type** on `messages.NathejkPaymentRequested.Method` and at the existing
   `"mobilepay"` call site, so the compiler is what stops a typo. Changing the Go field
   type from `string` to `types.PaymentMethod` must **not** change the JSON: the tag stays
   `json:"method"` and the encoded value stays a plain lowercase string, so old events
   deserialise unchanged.
3. **Document what an internal transfer is** on the constant, because it is the one
   payment kind with no provider behind it: money that a real provider payment already
   brought in, being re-attributed from one order to another. It is not a card
   transaction, not a refund, and it must never be handed to a payment provider.

### What NOT to do

- **Do not make the provider path generic over method.** `tables/payment/commands.go`'s
  `Request` talks to a provider (`repository.provider`) and only MobilePay lives there.
  An internal transfer never goes through it. Introducing method-dispatch inside `Request`
  would imply providers for methods that have none.
- **Do not create the internal-transfer payments here.** Publishing them is task 159's
  job, which needs the transfer semantics and the paired orders. This task only makes the
  vocabulary exist and be typed. Keeping it separate is deliberate: it can land, be
  reviewed and be consumed with no behaviour change at all.
- **Do not widen the column or migrate existing rows.** `VARCHAR(99)` already holds any
  of these values and every existing row is already `mobilepay`. No schema change is
  expected in this task; if you find you need one, use `cqrs.EnsureColumn` (see
  `tables/order/table.go`) rather than editing `table.sql` alone, because
  `CREATE TABLE IF NOT EXISTS` does not alter an existing table and the change would be
  silently missing in every existing database.

## Notes

- **A transfer payment must land in a status that counts.** Every "has this been paid"
  computation in the codebase filters `status IN ('reserved','received')` — including the
  order read model's `paidAmount` subquery. A payment stuck at `requested` is invisible to
  all of them. That constrains task 159, not this one, but the constant's documentation is
  the right place to say it.
- The payment projector consumes `requested`, `reserved` and `received`
  (`tables/payment/consumer.go`). Only the **`requested`** branch carries `method` — it is
  an `INSERT ... ON DUPLICATE KEY UPDATE` that writes `method=%q`, while `reserved` and
  `received` are `UPDATE`s keyed on `reference` that do not touch it. So the method is set
  once, when the payment is first announced, and a payment that never had a `requested`
  event has no method and no row at all.
- `types.PaymentStatusRejected` and `types.PaymentStatusTimedout` exist with no producer;
  don't be misled into thinking every constant in this area is live.

### Contract that hq will depend on

- The exact wire string for the new method. hq will display and filter on it, so name it
  in the progress log: the proposal is `internal-transfer`, but if you choose
  `internal_transfer` or `transfer`, that is the value hq must be told about.
- `types.PaymentMethod` being exported and importable, so hq can compare against the
  constant instead of a literal.

### Impact on other consumers

Both tilmelding and hq read the `payment` table; tilmelding also writes it. This change is
**purely additive**: one new constant, one Go type where a `string` was, and no change to
JSON, SQL, or behaviour. Nothing that works today works differently afterwards. The one
way to break tilmelding is to change the `mobilepay` wire value — don't.

## Acceptance Criteria

- [x] `types.PaymentMethod` exists with constants for `mobilepay` and the internal-transfer
      value, documented, following the conventions of the neighbouring types
- [x] `messages.NathejkPaymentRequested.Method` uses the type; JSON tag and encoded
      representation are unchanged
- [x] The existing `"mobilepay"` literal in `tables/payment/commands.go` is replaced by the
      constant, and its wire value is byte-identical to before
- [x] A round-trip test proves an event serialised before this change still deserialises
      (i.e. an unknown or legacy method string does not error)
- [x] No provider dispatch, no new payments published, no schema change
- [x] The chosen wire string for internal transfer recorded in the progress log

## Progress Log

<!-- Append entries here — never edit or delete existing entries -->

- 2026-09-07 — Created from hq PRD 012 §8 obstacle 3. Written to be lifted into shared-go.
  Independent of tasks 156 and 158; task 159 depends on this one.
- 2026-09-07 — Lifted into shared-go and picked up.
- 2026-09-07 — **Wire string decided: `internal-transfer`** (the proposal, unchanged).
  Hyphenated rather than `internal_transfer` because the neighbouring wire vocabulary is
  hyphen/lowercase, and spelled out rather than `transfer` because "transfer" alone reads
  like a provider transfer, which is the one thing it is not.
- 2026-09-07 — Added `types.PaymentMethod` to `types/payment.go` with
  `PaymentMethodNone` (`""`), `PaymentMethodMobilePay` (`"mobilepay"`) and
  `PaymentMethodInternalTransfer` (`"internal-transfer"`), plus `Valid()` and `String()`.
  `Valid()` excludes the zero value, matching `types.MemberStatus.Valid()`: an unset method
  is readable (769 legacy rows, and any event predating the field) but not publishable. The
  internal-transfer constant documents that it has no provider, must never be handed to
  one, is not a refund, must reach `reserved`/`received` to count towards any paid-amount,
  and that its origin should stay nameable (task 158).
- 2026-09-07 — `messages.NathejkPaymentRequested.Method` is now `types.PaymentMethod`; JSON
  tag unchanged. `tables/payment/commands.go` stamps `types.PaymentMethodMobilePay`. No
  provider dispatch was added to `Request`, no payment is published, no schema change —
  `VARCHAR(99)` already holds both values.
- 2026-09-07 — Also typed the **read** side: `payment.Payment.Method` is
  `types.PaymentMethod` instead of `string`, matching `Status types.PaymentStatus` in the
  same struct, so a reader can compare against the constant without a conversion.
  `database/sql` scans into it exactly as it already does for `Status`, and the `db`/`json`
  tags are unchanged. **This is the one source-compatibility note for downstream repos:**
  code that assigns `p.Method` to a `string` variable now needs `string(p.Method)`. No
  runtime, wire or schema behaviour changes.
- 2026-09-07 — Tests: `messages/payment_test.go` asserts the encoded form is a plain
  `"method":"mobilepay"` and that a requested event decodes and round-trips with a known
  method, **no** method field (→ `PaymentMethodNone`) and a method this build does not know
  (→ preserved verbatim, no error — an event that fails to decode is an event that stops
  being projected). `types/payment_test.go` pins the wire literals and `Valid()`.
  `tables/payment/commands_test.go`'s `TestRequestAuthorisesAndPublishes` now asserts the
  stamped method is byte-identical to `"mobilepay"`. Full `go test ./...` green.
- 2026-09-07 — **Contract for hq:** the internal-transfer wire value is
  `internal-transfer`; the type is `types.PaymentMethod` with `PaymentMethodMobilePay` /
  `PaymentMethodInternalTransfer` / `PaymentMethodNone`, and `payment.Payment.Method` is
  now that type.
- 2026-09-07 — Completed. Vocabulary exists and is typed; nothing produces an
  internal-transfer payment yet (task 159).
