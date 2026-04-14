-- 100k users, 500k orders. Enough volume that a Seq Scan on orders
-- shows a meaningful cost delta vs an index scan in EXPLAIN.

INSERT INTO users (email, created_at)
SELECT
    'user' || g || '@example.com',
    now() - (random() * interval '365 days')
FROM generate_series(1, 100000) AS g;

INSERT INTO orders (customer_id, email, total_cents, status, created_at)
SELECT
    ((random() * 99999)::int + 1),
    'user' || ((random() * 99999)::int + 1) || '@example.com',
    (random() * 100000)::bigint,
    (ARRAY['pending','paid','shipped','refunded'])[floor(random() * 4 + 1)::int],
    now() - (random() * interval '365 days')
FROM generate_series(1, 500000);

-- Guarantee at least one match for the canonical demo query
-- (WHERE lower(o.email) = 'a@b.com'), so --analyze returns a non-empty plan.
INSERT INTO orders (customer_id, email, total_cents, status, created_at)
VALUES (1, 'a@b.com', 4200, 'paid', now() - interval '5 days');
