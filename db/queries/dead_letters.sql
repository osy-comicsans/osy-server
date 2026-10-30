SELECT id, original_message_id, event_id, payload, error, delivery_attempts,
       status, replay_count, last_replay_error, last_replayed_at, created_at, resolved_at
FROM dead_letters
WHERE ($1 = '' OR status = $1)
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
