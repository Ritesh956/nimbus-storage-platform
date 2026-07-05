-- Post-v1 backlog #9 (DLQ visibility): events that exhausted NATS
-- redelivery (maxDeliver, events.Subscribe) land here instead of silently
-- stopping. Postgres rather than a NATS DLQ subject because the point is
-- *visibility and retry* — a queryable table with a status column beats a
-- second stream that would itself need a consumer, and retry is just a
-- republish of the stored payload. Owned by internal/events (it's eventing
-- infrastructure, not any domain module's data).
CREATE TABLE dead_events (
    id         uuid PRIMARY KEY,
    subject    text NOT NULL,
    payload    jsonb NOT NULL,
    error      text NOT NULL,
    deliveries int NOT NULL,
    status     text NOT NULL DEFAULT 'dead' CHECK (status IN ('dead', 'retried')),
    created_at timestamptz NOT NULL DEFAULT now(),
    retried_at timestamptz
);
CREATE INDEX idx_dead_events_created ON dead_events (created_at DESC);
