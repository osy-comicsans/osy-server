CREATE TABLE user_documents (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('driver_license', 'vehicle_registration')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'verified', 'rejected')),
    rejection_reason TEXT,
    source TEXT NOT NULL CHECK (source IN ('scan', 'upload')),
    capture_language TEXT NOT NULL CHECK (capture_language IN ('fr', 'ar')),
    mime_type TEXT NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'application/pdf')),
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
    file_ciphertext BYTEA NOT NULL,
    fields_ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, type)
);

CREATE INDEX user_documents_status_idx ON user_documents (status, updated_at);
