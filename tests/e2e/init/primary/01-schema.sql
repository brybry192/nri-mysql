-- E2E test schema: creates realistic tables and indexes so the integration
-- collects richer metrics (table sizes, InnoDB stats, etc.) than an empty DB.

SET cte_max_recursion_depth = 10000;

CREATE TABLE IF NOT EXISTS orders (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    customer_id INT          NOT NULL,
    status      VARCHAR(20)  NOT NULL DEFAULT 'pending',
    total_cents INT          NOT NULL,
    created_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_orders_customer (customer_id),
    INDEX idx_orders_status (status),
    INDEX idx_orders_created (created_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS customers (
    id         INT AUTO_INCREMENT PRIMARY KEY,
    email      VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS products (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    sku         VARCHAR(50)  NOT NULL UNIQUE,
    name        VARCHAR(255) NOT NULL,
    price_cents INT          NOT NULL
) ENGINE=InnoDB;

-- Seed with enough rows that the integration reports non-trivial sizes.
-- MySQL doesn't have generate_series, so we use a recursive CTE.

INSERT IGNORE INTO customers (email)
WITH RECURSIVE seq AS (
    SELECT 1 AS i
    UNION ALL
    SELECT i + 1 FROM seq WHERE i < 500
)
SELECT CONCAT('user', i, '@example.com') FROM seq;

INSERT IGNORE INTO products (sku, name, price_cents)
WITH RECURSIVE seq AS (
    SELECT 1 AS i
    UNION ALL
    SELECT i + 1 FROM seq WHERE i < 100
)
SELECT CONCAT('SKU-', i), CONCAT('Product ', i), FLOOR(RAND() * 10000) FROM seq;

INSERT INTO orders (customer_id, status, total_cents)
WITH RECURSIVE seq AS (
    SELECT 1 AS i
    UNION ALL
    SELECT i + 1 FROM seq WHERE i < 5000
)
SELECT
    FLOOR(RAND() * 499 + 1),
    ELT(FLOOR(RAND() * 4 + 1), 'pending', 'paid', 'shipped', 'cancelled'),
    FLOOR(RAND() * 50000 + 100)
FROM seq;
