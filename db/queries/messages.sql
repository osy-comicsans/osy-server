SELECT id, provider, provider_message_id, sender, recipient, message, received_at,
       processing_status, processed_at, processing_error, created_at
FROM sms_messages
WHERE ($1 = '' OR sender = $1)
  AND ($2::timestamptz IS NULL OR received_at >= $2)
  AND ($3::timestamptz IS NULL OR received_at <= $3)
  AND ($4 = '' OR provider_message_id = $4 OR id::text = $4)
ORDER BY received_at DESC, id DESC
LIMIT $5 OFFSET $6;
