# 158 — [shared-go] Payment provenance: where transferred money originally came from

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

A payment is about to stop always meaning "money arrived from a provider". A second kind
is coming — an **internal transfer**, which re-attributes money that a real provider
payment already brought in, moving it from one order to another (task 157 adds the method,
task 159 creates them).

The moment that exists, `payment` contains rows whose audit trail **dead-ends**. You can
see that an order was covered by an internal transfer, and nothing more: not which real
payment the money came from, not who actually paid it, not when. The question "who paid
for this?" becomes unanswerable for exactly the records where somebody is most likely to
ask it.

This task makes provenance a **recorded fact on the payment**, before any transfers exist
to be untraceable.

### Requirements

1. **A transfer payment references the payment whose money it moves.** Not the counterpart
   order — that pairs the two halves of one transfer, which is task 159's concern — but the
   payment through which the funds originally entered the system.

2. **The reference must resolve to the root, not one hop.** Money can be transferred more
   than once: A → B, then B → C. A transfer whose source is *itself* a transfer must
   inherit the original provider payment's reference rather than naming its immediate
   predecessor. Otherwise provenance degrades with every move and the chain has to be
   walked backwards through an unknown number of hops to answer a simple question.
   Recording the immediate predecessor **as well** is fine; losing the root is not.

3. **"Unknown" must be representable, and must not block the transfer.** Identifying the
   source payment will often fail, and this is the normal case rather than an edge case:
   `payment.orderForeignKey` is polymorphic, and most payments are not reachable from an
   order at all. From `tables/payment/table.go`:

   > since the order entity landed `OrderForeignKey` is an order id and `OrderType` is
   > `"order"`, but the 769 rows that predate it hold a team or user id with `OrderType`
   > naming the kind (`"patrulje"`, `"klan"`, `"gøgler"`).

   In live 2026 data, of 189 paid orders only 44 are linked by order id — the other 151 are
   reachable only through their owner. So a transfer must still be creatable when the
   source cannot be pinned down, with provenance recorded as **explicitly unknown** rather
   than silently absent. Those two states must be distinguishable: "we looked and could not
   tell" is information; a zero value is not.

4. **It must survive a full replay.** Read models here are rebuilt from JetStream on every
   start. Provenance therefore belongs in the **event payload**, resolved once when the
   transfer is created — not computed by the projector from a read model that will look
   different by the time it is replayed.

### Where to put it

Decide, and record the reasoning:

- **A column on `payment`** — best if it will be queried, and it will be: *show me every
  transfer whose money came from payment X* is a lookup, not a narrative. An indexed
  column beats digging through JSON.
- **`payment.operations`** — already `JSON NOT NULL DEFAULT ('[]')`, already an
  append-only trail with one entry per transition, and already described as the thing from
  which "a payment captured in several parts can be reconstructed from the projection
  alone". A natural home for the human-readable chain, a poor one for an indexed join.

These are not exclusive: a column for the root reference, the chain in `operations`.

**If you add a column, use the existing migration idiom** — `CREATE TABLE IF NOT EXISTS`
never alters an existing table, so a column added only to `table.sql` will be silently
missing from every existing database while appearing to work in a freshly created one. The
house pattern is `cqrs.EnsureColumn` / `cqrs.EnsureIndex`, called from the package's `New`:

```go
	if err := cqrs.EnsureColumn(r, w, "order_line", "memberId",
		"memberId VARCHAR(64) NOT NULL DEFAULT '' AFTER productName"); err != nil {
		log.Fatalf("Error migrating order_line.memberId %q", err)
	}
	if err := cqrs.EnsureIndex(r, w, "order_line", "idx_order_line_member",
		"ALTER TABLE order_line ADD INDEX idx_order_line_member (memberId)"); err != nil {
		log.Fatalf("Error migrating order_line.idx_order_line_member %q", err)
	}
```

`tables/payment/table.go` also has an older hand-rolled version of the same idea
(`ALTER TABLE payment ADD COLUMN IF NOT EXISTS operations …`), whose comment explains why
it is issued as its own statement rather than appended to `table.sql`. Prefer
`cqrs.EnsureColumn`.

### Where it goes in the event

Only the **`requested`** branch of the payment projector writes the descriptive fields —
it is an `INSERT ... ON DUPLICATE KEY UPDATE` writing `method`, `orderForeignKey`,
`orderType` and friends, while `reserved` and `received` are `UPDATE`s keyed on `reference`
that do not touch them. So provenance belongs on `messages.NathejkPaymentRequested`, set
when the payment is announced.

Adding a field there is safe for existing events: absent JSON decodes to the zero value,
which is why the zero value must mean "not applicable / not a transfer" and **not** be
confused with "unknown source". A provider payment has no provenance because it *is* the
provenance.

## Notes

- Do not attempt to backfill provenance for the 769 legacy rows or infer it for existing
  MobilePay payments. A provider payment is its own root.
- Resist making the source a foreign key with referential intent. It is a reference for
  humans and reports; the source payment may be from a previous season, and nothing should
  refuse to record a transfer because the lookup failed.
- Whether to record the paying **team/owner** as well as the payment reference is worth a
  moment's thought: the reference is precise, but "betalt af Patrulje 12" is what an
  operator actually wants to read, and the owner of a legacy payment is often the *only*
  thing knowable about it.

### Contract that hq will depend on

hq will display this ("betalt af Patrulje 12 (MobilePay, 4. juni)") and may filter on it.
Record in the progress log:

- the JSON field name(s) added to `NathejkPaymentRequested`
- the column name and type, if a column was added
- the exact representation of **unknown**
- the shape of the chain entry in `operations`, if used

### Impact on other consumers

Additive: one new optional event field and, if chosen, one new column added through a
guarded migration. Existing producers (tilmelding's provider payments) set nothing and
behave identically; existing readers ignore what they don't select. No existing row
changes meaning. The only way to break something is to reuse an existing field for a new
purpose — don't.

## Acceptance Criteria

- [x] A payment can carry the reference of the payment whose money it moves, set at
      creation time and carried in the event payload
- [x] Provenance resolves to the **root** provider payment across at least two hops
      (A → B → C still names A), proven by a test
- [x] "Unknown source" is representable and distinguishable from "not a transfer", and does
      not prevent a payment being created
- [x] Events published before this change still deserialise unchanged
- [x] If a column was added, it is created through `cqrs.EnsureColumn` (or equivalent
      guarded migration) and verified to appear in a pre-existing database, not only a
      freshly created one
- [x] A full replay of the event log reproduces identical provenance (nothing derived from
      current read-model state)
- [x] Field names, column name, and the unknown representation recorded in the progress log

## Progress Log

<!-- Append entries here — never edit or delete existing entries -->

- 2026-09-07 — Created from hq PRD 012 §8 (Provenance) and §11 Q4. Written to be lifted
  into shared-go. Independent of tasks 156 and 157; task 159 depends on this one.
- 2026-09-07 — Lifted into shared-go and picked up.
- 2026-09-07 — **Decision: both homes, as the task's own "not exclusive" note suggests.**
  An indexed column for the root reference (the lookup) plus a JSON column for the full
  snapshot (the record). Not `operations`: its entries have a fixed `{type,amount,time}`
  shape that `OperationList` unmarshals, and a differently-shaped entry there would be a
  poor citizen of a trail whose whole point is one entry per transition. The redundancy
  between the two new columns is the same split this table already uses for `status`
  alongside `operations` — current-state summary next to the full record.
- 2026-09-07 — **Contract for hq — field, column and unknown names:**
  - Event field: `messages.NathejkPaymentRequested.Source *types.PaymentSource`,
    `json:"source,omitempty"`. Its JSON keys are `reference`, `via`, `ownerType`,
    `ownerId`, `method`, `paidAt` (all but `reference` are `omitempty`).
  - Columns: `payment.sourceReference VARCHAR(99) NOT NULL DEFAULT ''` (indexed as
    `idx_payment_source`) and `payment.source JSON NOT NULL DEFAULT ('{}')`.
  - Read model: `payment.Payment.SourceReference string` and
    `payment.Payment.Source payment.SourceRecord` (embeds `types.PaymentSource`, with
    `Scan`/`Value` for the JSON column).
  - **Three states, all distinguishable.** Not a transfer: `Source == nil` on the event,
    `sourceReference = ''` and `source = '{}'` in the row. Known: the root's reference.
    Unknown: the exact string **`unknown`** (`types.PaymentSourceUnknown`), in both the
    field and the column. Safe as a sentinel because references are twelve characters of
    Crockford base32 (uppercase + digits), so no lowercase seven-character string is
    reachable. `types.PaymentSource.Known()` is the check to use.
  - `operations` is **not** used for the chain; `via` carries the immediate predecessor.
- 2026-09-07 — Resolution lives in `tables/payment/provenance.go`:
  `SourceOf(ctx, reference) *types.PaymentSource`, exposed through a new narrow
  `payment.SourceResolver` interface rather than being added to `Queries` — adding a method
  to `Queries` would break every fake implementing it in another repo, and a transfer's
  creator needs nothing else from the read side. `*table` satisfies it, so the composition
  root wires the same value.

  It never returns nil and never returns an error: an unresolvable source is
  `types.UnknownPaymentSource()`. That is deliberate, per the task — refusing to record a
  transfer because a lookup came back empty would be the wrong trade every time.

  **Root, not one hop:** because every transfer stores the *root* rather than its
  predecessor, resolution costs exactly one extra read and A → B → C → … still names A,
  with the immediate predecessor in `via`. Inconsistent data therefore cannot send it into
  an unbounded walk. The owner is recovered from whichever linkage the row actually uses —
  through `orders` for current payments, and straight off the polymorphic
  `orderForeignKey`/`orderType` pair for legacy ones, where the foreign key *is* the team
  id. That second branch is what makes provenance worth anything for the 151-of-189 case
  in the task description.
- 2026-09-07 — Deliberately **no owner name** on `PaymentSource`, only `ownerType` +
  `ownerId`. Names live in the team read models, which are not this package's to read and
  which hq can join for a *current* name; the id is the part that stays true. hq composes
  "betalt af Patrulje 12 (MobilePay, 4. juni)" from `ownerId` + `method` + `paidAt`.
- 2026-09-07 — Replay-stability: provenance is resolved once, by the transfer's creator,
  and carried in the event payload; the projector only copies it. Nothing is derived from
  read-model state at projection time, so a rebuild years later reproduces the same
  provenance. The `requested` branch is the only one that writes it, matching the fact that
  `reserved`/`received` are UPDATEs keyed on reference.
- 2026-09-07 — Migration: `sourceReference`, `source` and `idx_payment_source` are added
  through `cqrs.EnsureColumn` / `cqrs.EnsureIndex` in `payment.New`, **and** declared in
  `table.sql` for fresh databases. Verified with `sqlmock` (added as a test dependency; it
  was already in the module cache) that a **pre-existing** payment table — where
  `CREATE TABLE IF NOT EXISTS` changes nothing — still gets all three, and that nothing is
  altered when the existence checks report them present.
- 2026-09-07 — Tests: `tables/payment/provenance_test.go` (root across two hops and across
  a longer chain, with the lookup count asserting one extra read; the three unknown paths;
  an unprojected root keeping its reference; legacy owner recovery; the generated SQL),
  `tables/payment/consumer_test.go` (both columns written, upsert refreshes them, unknown
  written explicitly, an omitted reference normalised to unknown, transitions never touch
  provenance), `tables/payment/migration_test.go`, and `messages/payment_test.go` (absent
  source decodes to nil, unknown survives as a value, a known source round-trips whole, a
  provider payment emits no `source` key at all). `go test ./...`, `go vet ./...` and
  `gofmt -l .` all clean.
- 2026-09-07 — Completed. Nothing produces a transfer payment yet; task 159 is the caller.
