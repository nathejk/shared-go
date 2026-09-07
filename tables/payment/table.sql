CREATE TABLE IF NOT EXISTS payment (
    reference VARCHAR(99) NOT NULL,
    receiptEmail VARCHAR(99) NOT NULL DEFAULT "",
    returnUrl VARCHAR(999) NOT NULL DEFAULT "",
    year VARCHAR(99) NOT NULL DEFAULT "",
    currency VARCHAR(9),
    amount INT NOT NULL DEFAULT 0,
    method VARCHAR(99) NOT NULL DEFAULT "",
    createdAt VARCHAR(99),
    changedAt VARCHAR(99),
    status VARCHAR(99),
    orderForeignKey VARCHAR(99) NOT NULL DEFAULT "",
    orderType VARCHAR(99) NOT NULL DEFAULT "",
    operations JSON NOT NULL DEFAULT ('[]'),
    -- sourceReference / source record provenance for an internal transfer: the
    -- root provider payment whose money this payment moves. Added through
    -- cqrs.EnsureColumn in New (see table.go) as well as here, because CREATE
    -- TABLE IF NOT EXISTS never alters an existing table — without the migration
    -- these would be silently missing from every database that already has a
    -- payment table while appearing to work in a fresh one.
    --
    -- '' means "not a transfer"; 'unknown' means "is a transfer, source could not
    -- be identified". See types.PaymentSource.
    sourceReference VARCHAR(99) NOT NULL DEFAULT "",
    source JSON NOT NULL DEFAULT ('{}'),
    PRIMARY KEY (reference),
    -- Every paid-amount sum joins payment on orderForeignKey and filters
    -- status IN ('reserved','received'); the PK (reference) serves neither.
    KEY idx_payment_order (orderForeignKey, status),
    -- Per-year status reporting (paid/pending totals for a season).
    KEY idx_payment_year_status (year, status),
    -- "Show me every transfer whose money came from payment X" is a lookup, not
    -- a narrative, so the root reference gets an index of its own.
    KEY idx_payment_source (sourceReference)
);
