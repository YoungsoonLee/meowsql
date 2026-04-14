-- Without ANALYZE, pg_class.reltuples is 0 and the planner's cost estimates
-- are garbage — which would make the context we send to the agent lie.
VACUUM ANALYZE users;
VACUUM ANALYZE orders;
