INSERT INTO sms_messages (
    provider, provider_message_id, sender, recipient, message, received_at
) VALUES
    ('africastalking', '900001', '+254700000001', 'OSYN', 'First seeded message', now() - interval '2 minutes'),
    ('africastalking', '900002', '+254700000002', 'OSYN', 'Second seeded message', now() - interval '1 minute')
ON CONFLICT (provider, provider_message_id) DO NOTHING;

WITH event_id AS (
    SELECT gen_random_uuid() AS id
)
INSERT INTO outbox_events (id, aggregate_id, event_type, subject, payload)
SELECT
    event_id.id,
    message.provider_message_id,
    'sms.received',
    'sms.incoming',
    jsonb_build_object(
        'event_id', event_id.id,
        'message_id', message.id,
        'provider', message.provider,
        'provider_message_id', message.provider_message_id,
        'sender', message.sender,
        'recipient', message.recipient,
        'message', message.message,
        'received_at', message.received_at
    )
FROM sms_messages message
CROSS JOIN event_id
WHERE message.provider_message_id IN ('900001', '900002')
  AND NOT EXISTS (
      SELECT 1 FROM outbox_events event
      WHERE event.aggregate_id = message.provider_message_id
        AND event.event_type = 'sms.received'
  );

INSERT INTO ingestion_checkpoints (provider, last_received_id, updated_at)
VALUES ('africastalking', 900002, now())
ON CONFLICT (provider) DO UPDATE SET
    last_received_id = GREATEST(ingestion_checkpoints.last_received_id, EXCLUDED.last_received_id),
    updated_at = now();
