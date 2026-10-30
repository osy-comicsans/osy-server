WITH claimed AS (
    SELECT id
    FROM outbox_events
    WHERE (status = 'pending' AND next_attempt_at <= now())
       OR (status = 'processing' AND locked_until IS NOT NULL AND locked_until < now())
    ORDER BY next_attempt_at, created_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox_events event
SET status = 'processing', locked_until = $2, updated_at = now()
FROM claimed
WHERE event.id = claimed.id
RETURNING event.*;
