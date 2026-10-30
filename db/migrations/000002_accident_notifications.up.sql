CREATE TABLE accident_reports (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    sms_message_id BIGINT REFERENCES sms_messages(id) ON DELETE SET NULL,
    provider_message_id TEXT NOT NULL,
    sender TEXT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL CHECK (latitude >= -90 AND latitude <= 90),
    longitude DOUBLE PRECISION NOT NULL CHECK (longitude >= -180 AND longitude <= 180),
    damage_percentage DOUBLE PRECISION NOT NULL CHECK (damage_percentage >= 0 AND damage_percentage <= 100),
    radius_meters INTEGER NOT NULL CHECK (radius_meters > 0),
    raw_message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX accident_reports_created_idx ON accident_reports (created_at DESC);
CREATE INDEX accident_reports_provider_message_idx ON accident_reports (provider_message_id);

CREATE TABLE emergency_notifications (
    id UUID PRIMARY KEY,
    accident_report_id BIGINT NOT NULL REFERENCES accident_reports(id) ON DELETE CASCADE,
    service TEXT NOT NULL,
    target_phone TEXT NOT NULL,
    message TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    twilio_sid TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (accident_report_id, service)
);

CREATE INDEX emergency_notifications_status_idx ON emergency_notifications (status, created_at);
CREATE INDEX emergency_notifications_report_idx ON emergency_notifications (accident_report_id);
