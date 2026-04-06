-- Inventory database schema: a separate service with its own tables.

SET cte_max_recursion_depth = 10000;

CREATE TABLE IF NOT EXISTS warehouses (
    id       INT AUTO_INCREMENT PRIMARY KEY,
    name     VARCHAR(100) NOT NULL,
    location VARCHAR(255) NOT NULL
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS items (
    id           INT AUTO_INCREMENT PRIMARY KEY,
    sku          VARCHAR(50)  NOT NULL UNIQUE,
    name         VARCHAR(255) NOT NULL,
    warehouse_id INT          NOT NULL,
    quantity     INT          NOT NULL DEFAULT 0,
    INDEX idx_items_warehouse (warehouse_id),
    INDEX idx_items_sku (sku)
) ENGINE=InnoDB;

-- Seed data
INSERT IGNORE INTO warehouses (name, location) VALUES
    ('East Coast', 'us-east-1'),
    ('West Coast', 'us-west-2'),
    ('Central', 'us-central-1');

INSERT IGNORE INTO items (sku, name, warehouse_id, quantity)
WITH RECURSIVE seq AS (
    SELECT 1 AS i
    UNION ALL
    SELECT i + 1 FROM seq WHERE i < 2000
)
SELECT
    CONCAT('INV-', i),
    CONCAT('Item ', i),
    FLOOR(RAND() * 3 + 1),
    FLOOR(RAND() * 500)
FROM seq;
