-- Refresh optimizer statistics so EXPLAIN row estimates are realistic.
ANALYZE TABLE users, orders;
