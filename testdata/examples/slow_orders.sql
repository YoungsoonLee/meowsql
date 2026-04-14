-- Example slow query for demoing `meowsql analyze`.
-- Non-SARGable predicate (lower() on an unindexed expression) typically
-- forces a Seq Scan even when a btree index on email exists.
SELECT o.id, o.total_cents, o.created_at
FROM orders o
WHERE lower(o.email) = 'a@b.com'
  AND o.created_at >= now() - interval '30 days'
ORDER BY o.created_at DESC
LIMIT 50;
