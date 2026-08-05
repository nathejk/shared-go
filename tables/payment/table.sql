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
    PRIMARY KEY (reference)
);
