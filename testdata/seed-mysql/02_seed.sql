-- 100k users, 500k orders using a recursive CTE (MySQL 8+).
-- Raise cte_max_recursion_depth so the generator can emit >1k rows.

SET SESSION cte_max_recursion_depth = 1000000;

INSERT INTO users (email, created_at)
WITH RECURSIVE seq(n) AS (
    SELECT 1
    UNION ALL
    SELECT n + 1 FROM seq WHERE n < 100000
)
SELECT CONCAT('user', n, '@example.com'),
       NOW() - INTERVAL FLOOR(RAND() * 365) DAY
FROM seq;

INSERT INTO orders (customer_id, email, total_cents, status, created_at)
WITH RECURSIVE seq(n) AS (
    SELECT 1
    UNION ALL
    SELECT n + 1 FROM seq WHERE n < 500000
)
SELECT
    FLOOR(RAND() * 99999) + 1,
    CONCAT('user', FLOOR(RAND() * 99999) + 1, '@example.com'),
    FLOOR(RAND() * 100000),
    ELT(FLOOR(RAND() * 4) + 1, 'pending', 'paid', 'shipped', 'refunded'),
    NOW() - INTERVAL FLOOR(RAND() * 365) DAY
FROM seq;

-- Guarantee at least one match for the canonical demo query
-- (WHERE LOWER(o.email) = 'a@b.com'), so --analyze returns a non-empty plan.
INSERT INTO orders (customer_id, email, total_cents, status, created_at)
VALUES (1, 'a@b.com', 4200, 'paid', NOW() - INTERVAL 5 DAY);
