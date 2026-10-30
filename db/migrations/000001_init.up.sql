CREATE TABLE sms_messages (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_message_id TEXT NOT NULL,
    sender TEXT NOT NULL DEFAULT '',
    recipient TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    received_at TIMESTAMPTZ NOT NULL,
    processing_status TEXT NOT NULL DEFAULT 'received' CHECK (processing_status IN ('received', 'processed', 'failed')),
    processed_at TIMESTAMPTZ,
    processing_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_message_id)
);

CREATE INDEX sms_messages_sender_received_idx ON sms_messages (sender, received_at DESC);
CREATE INDEX sms_messages_received_idx ON sms_messages (received_at DESC);
CREATE INDEX sms_messages_status_idx ON sms_messages (processing_status);

CREATE TABLE ingestion_checkpoints (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL UNIQUE,
    last_received_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    subject TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'published', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX outbox_events_dispatch_idx ON outbox_events (status, next_attempt_at);
CREATE INDEX outbox_events_lease_idx ON outbox_events (locked_until);

CREATE TABLE processed_events (
    event_id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_message_id TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE dead_letters (
    id UUID PRIMARY KEY,
    original_message_id BIGINT REFERENCES sms_messages(id) ON DELETE SET NULL,
    event_id TEXT NOT NULL UNIQUE,
    payload JSONB NOT NULL,
    error TEXT NOT NULL,
    delivery_attempts INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'unresolved' CHECK (status IN ('unresolved', 'resolved')),
    replay_count INTEGER NOT NULL DEFAULT 0,
    last_replay_error TEXT,
    last_replayed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX dead_letters_status_created_idx ON dead_letters (status, created_at DESC);
