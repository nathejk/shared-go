CREATE TABLE IF NOT EXISTS orders (
    orderId       VARCHAR(99)  NOT NULL,
    year          VARCHAR(8)   NOT NULL,
    ownerType     VARCHAR(32)  NOT NULL,
    ownerId       VARCHAR(99)  NOT NULL,
    status        VARCHAR(32)  NOT NULL DEFAULT 'open',
    currency      VARCHAR(8)   NOT NULL DEFAULT 'DKK',
    totalAmount   INT          NOT NULL DEFAULT 0,
    cancelReason  VARCHAR(255) NOT NULL DEFAULT '',
    createdAt     VARCHAR(99),
    changedAt     VARCHAR(99),
    PRIMARY KEY (orderId),
    INDEX idx_orders_owner (year, ownerType, ownerId, status)
);

CREATE TABLE IF NOT EXISTS order_line (
    orderId       VARCHAR(99)  NOT NULL,
    lineId        VARCHAR(128) NOT NULL,
    productSku    VARCHAR(64)  NOT NULL,
    productName   VARCHAR(255) NOT NULL,
    memberId      VARCHAR(64)  NOT NULL DEFAULT '',
    unitPrice     INT          NOT NULL DEFAULT 0,
    quantity      INT          NOT NULL DEFAULT 0,
    lineTotal     INT          NOT NULL DEFAULT 0,
    origin        VARCHAR(32)  NOT NULL DEFAULT 'derived',
    attributes    TEXT,
    createdAt     VARCHAR(99),
    changedAt     VARCHAR(99),
    PRIMARY KEY (orderId, lineId),
    INDEX idx_order_line_sku (productSku),
    INDEX idx_order_line_member (memberId)
);
