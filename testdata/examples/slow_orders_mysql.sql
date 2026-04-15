-- MySQL-dialect version of the canonical slow query.
-- Non-SARGable predicate (LOWER() on an unindexed expression) typically
-- forces a full scan even when a btree index on email exists.
SELECT o.id, o.total_cents, o.created_at
FROM orders o
WHERE LOWER(o.email) = 'a@b.com'
  AND o.created_at >= NOW() - INTERVAL 30 DAY
ORDER BY o.created_at DESC
LIMIT 50;
