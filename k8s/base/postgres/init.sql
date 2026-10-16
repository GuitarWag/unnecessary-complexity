-- Runs once, when Postgres initialises, so the slot exists before the shortener's
-- first write. The WAL then keeps every outbox event until Debezium first connects.
-- Debezium runs with snapshot.mode=no_data and streams from this slot's position.
--
-- The publication comes first. pgoutput looks it up in the catalog as of each
-- decoded change, so a change from before it existed would stop the stream for
-- good. The shortener's migration adds the outbox table to it.
CREATE PUBLICATION shortener_outbox WITH (publish = 'insert');
SELECT pg_create_logical_replication_slot('shortener_outbox', 'pgoutput');
