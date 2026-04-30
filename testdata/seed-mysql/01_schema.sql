-- Intentionally under-indexed schema, matching the PostgreSQL demo.
-- The gaps (no index on orders.email, no composite on customer_id + created_at)
-- are the whole point — the agent must surface them.

DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
    id         BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email      VARCHAR(255) NOT NULL,
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY users_email_idx (email)
) ENGINE=InnoDB;

CREATE TABLE orders (
    id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    customer_id BIGINT       NOT NULL,
    email       VARCHAR(255) NOT NULL,
    total_cents BIGINT       NOT NULL,
    status      VARCHAR(32)  NOT NULL,
    created_at  DATETIME     NOT NULL,
    CONSTRAINT orders_customer_fk FOREIGN KEY (customer_id) REFERENCES users(id)
) ENGINE=InnoDB;
