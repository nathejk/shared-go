CREATE TABLE IF NOT EXISTS product (
    sku           VARCHAR(64)  NOT NULL,
    year          VARCHAR(8)   NOT NULL,
    name          VARCHAR(255) NOT NULL,
    kind          VARCHAR(32)  NOT NULL,
    unitPrice     INT          NOT NULL DEFAULT 0,
    currency      VARCHAR(8)   NOT NULL DEFAULT 'DKK',
    eligibleFor   VARCHAR(255) NOT NULL DEFAULT '*',
    sizes         VARCHAR(255) NOT NULL DEFAULT '',
    stock         INT          NULL,
    active        TINYINT(1)   NOT NULL DEFAULT 1,
    createdAt     VARCHAR(99),
    changedAt     VARCHAR(99),
    PRIMARY KEY (sku, year)
);
