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
    PRIMARY KEY (reference),
    -- Every paid-amount sum joins payment on orderForeignKey and filters
    -- status IN ('reserved','received'); the PK (reference) serves neither.
    KEY idx_payment_order (orderForeignKey, status),
    -- Per-year status reporting (paid/pending totals for a season).
    KEY idx_payment_year_status (year, status)
);
