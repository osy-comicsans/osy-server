# OSYN

OSYN is a production-oriented Go SMS ingestion platform. It polls Africa's Talking's Fetch Messages API, persists messages through a transactional PostgreSQL inbox/outbox, publishes events to NATS JetStream, and processes them with independent durable pull workers. Failures are retried with exponential backoff and jitter, then stored in PostgreSQL and published to a dedicated dead letter stream.

## Architecture

```text
Africa's Talking Fetch Messages API
              |
              v
         OSYN Fetcher
              |
              v
          PostgreSQL
       (Inbox + Outbox)
              |
              v
       Outbox Publisher
              |
              v
        NATS JetStream
              |
              v
       Durable Pull Worker
              |
              v
       PostgreSQL Messages
              |
       +------+------+
       |             |
    Success        Failure
       |             |
      ACK       Retry with delay
                     |
              Maximum attempts
                     |
                     v
                 DLQ Stream
                     |
                     v
             Dead Letter Storage
```

The four long-running processes are independent binaries:

- `cmd/api` exposes the authenticated HTTP API, health/readiness checks, metrics, and Swagger UI.
- `cmd/fetcher` polls Africa's Talking and atomically writes the inbox, outbox, and ingestion checkpoint.
- `cmd/publisher` claims outbox rows with `FOR UPDATE SKIP LOCKED` and publishes acknowledged JetStream messages.
- `cmd/worker` consumes `sms.incoming` with a durable pull consumer and ACKs only after database processing or DLQ persistence and publication succeeds.

PostgreSQL is the source of truth. NATS is never used as primary storage.

## Requirements

- Docker with Docker Compose
- Go 1.26 or newer for local development
- Africa's Talking sandbox or production credentials
- Twilio account, sender, and E.164 recipients for live emergency delivery
- `make` for convenience targets

## Quick start

1. Create the environment file and set your values.

```sh
cp .env.example .env
```

At minimum, set a real `AFRICASTALKING_API_KEY` and replace `API_KEY=change-me`.

Sandbox and live credentials cannot be mixed. Sandbox requires `AFRICASTALKING_USERNAME=sandbox` and a sandbox API key against `https://api.sandbox.africastalking.com`. Live requires the application username and a live API key against `https://api.africastalking.com`. Newly generated API keys may take a few minutes to become active across all Africa's Talking endpoints.

2. Start PostgreSQL, NATS JetStream, the API, fetcher, publisher, and worker.

```sh
docker compose up -d --build
```

3. Confirm the stack is running.

```sh
docker compose ps
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

4. Load sample data that exercises the outbox and worker without calling Africa's Talking.

```sh
make seed
curl -H 'X-API-Key: change-me' http://localhost:8080/api/v1/stats
curl -H 'X-API-Key: change-me' 'http://localhost:8080/api/v1/messages?page=1&page_size=20'
```

5. Open Swagger UI at:

```text
http://localhost:8080/swagger/index.html
```

Use the API key as an `X-API-Key` header or as `Authorization: Bearer <key>`.

## Runtime data

The example stack uses these local values:

| Item | Value |
| --- | --- |
| API URL | `http://localhost:8080` |
| API key | `change-me` from `.env.example` |
| PostgreSQL | `postgres://osyn:osyn@localhost:5432/osyn?sslmode=disable` |
| PostgreSQL credentials | user `osyn`, password `osyn`, database `osyn` |
| NATS client URL | `nats://localhost:4222` |
| NATS monitoring | `http://localhost:8222` |
| SMS stream | `SMS_MESSAGES` |
| SMS subject | `sms.incoming` |
| DLQ stream | `SMS_DLQ` |
| DLQ subject | `sms.dlq` |

Docker services use the internal hostnames `postgres` and `nats`, which are already present in `.env.example`. When running binaries directly on the host, change `DATABASE_URL` and `NATS_URL` to `localhost`.

## Environment configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ENV` | `development` | Runtime environment |
| `APP_NAME` | `osyn` | Application name in logs and NATS connections |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `HTTP_PORT` | `8080` | API container listen port |
| `API_HOST_PORT` | `8080` | Host port mapped to the API |
| `HTTP_TIMEOUT` | `15s` | HTTP server read/write timeout |
| `SHUTDOWN_GRACE` | `20s` | Graceful API shutdown timeout |
| `API_KEY` | required | Protects all `/api/v1` endpoints |
| `AFRICASTALKING_USERNAME` | required | Africa's Talking username, usually `sandbox` |
| `AFRICASTALKING_API_KEY` | required | Africa's Talking API key |
| `AFRICASTALKING_BASE_URL` | sandbox URL | Sandbox or production API origin |
| `AFRICASTALKING_HTTP_TIMEOUT` | `15s` | Provider HTTP request timeout |
| `FETCH_INTERVAL` | `10s` | Poll interval |
| `FETCH_BATCH_SIZE` | `100` | Transaction batch size |
| `DATABASE_URL` | required | PostgreSQL connection URL |
| `POSTGRES_HOST_PORT` | `5432` | Host port mapped to PostgreSQL |
| `DB_MAX_CONNS` | `20` | Connection pool maximum |
| `MIGRATIONS_DIR` | `db/migrations` | SQL migration directory |
| `NATS_URL` | required | NATS connection URL |
| `NATS_HOST_PORT` | `4222` | Host port mapped to NATS |
| `NATS_MONITOR_HOST_PORT` | `8222` | Host port mapped to NATS monitoring |
| `NATS_SMS_STREAM` | `SMS_MESSAGES` | Durable incoming stream |
| `NATS_SMS_SUBJECT` | `sms.incoming` | Incoming subject |
| `NATS_DLQ_STREAM` | `SMS_DLQ` | Durable dead letter stream |
| `NATS_DLQ_SUBJECT` | `sms.dlq` | Dead letter subject |
| `NATS_STREAM_MAX_AGE` | `168h` | Stream retention |
| `NATS_DUPLICATE_WINDOW` | `24h` | JetStream duplicate detection window |
| `NATS_STREAM_MAX_MSGS` | `1000000` | Stream message limit |
| `NATS_STREAM_MAX_BYTES` | `1073741824` | Stream byte limit |
| `NATS_PUBLISH_TIMEOUT` | `10s` | Time to wait for JetStream publish acknowledgement |
| `WORKER_CONCURRENCY` | `5` | Concurrent worker processing loops |
| `WORKER_MAX_DELIVERIES` | `5` | Maximum processing attempts before DLQ |
| `WORKER_ACK_WAIT` | `30s` | JetStream acknowledgement wait |
| `WORKER_FETCH_WAIT` | `2s` | Pull fetch maximum wait |
| `WORKER_PROCESS_TIMEOUT` | `25s` | Database processing timeout |
| `WORKER_CONSUMER_DURABLE` | `osyn-worker` | Durable consumer name |
| `NOTIFICATION_WORKERS` | `4` | Concurrent emergency notification senders |
| `ACCIDENT_MAX_RADIUS_METERS` | `100000` | Maximum accepted accident radius |
| `EMERGENCY_CIVIL_PROTECTION_PHONE` | `14` | Civil Protection destination |
| `EMERGENCY_GENDARMERIE_PHONE` | `1055` | National Gendarmerie destination |
| `EMERGENCY_POLICE_PHONE` | `1548` | National Police destination |
| `TWILIO_DRY_RUN` | `true` | Log alerts without calling Twilio |
| `TWILIO_ACCOUNT_SID` | required for live | Twilio account SID |
| `TWILIO_AUTH_TOKEN` | required for live | Twilio auth token |
| `TWILIO_FROM_NUMBER` | required for live | E.164 Twilio sender number |
| `TWILIO_MESSAGING_SERVICE_SID` | optional | Alternative Twilio sender service |
| `TWILIO_BASE_URL` | `https://api.twilio.com` | Twilio API origin |
| `TWILIO_HTTP_TIMEOUT` | `15s` | Twilio HTTP timeout |
| `WHATSAPP_NOTIFICATIONS_ENABLED` | `false` | Send operator WhatsApp confirmations |
| `TWILIO_WHATSAPP_FROM` | sandbox sender | Twilio WhatsApp sender |
| `OPERATOR_WHATSAPP_NUMBER` | required if enabled | Operator WhatsApp destination |
| `WHATSAPP_DRY_RUN` | `true` | Log instead of sending WhatsApp |
| `RETRY_INITIAL_DELAY` | `2s` | First retry delay |
| `RETRY_MAX_DELAY` | `5m` | Retry delay cap |
| `RETRY_MAX_ATTEMPTS` | `5` | Retry attempt limit |
| `OUTBOX_PUBLISH_INTERVAL` | `1s` | Publisher cycle interval |
| `OUTBOX_BATCH_SIZE` | `100` | Events claimed per cycle |
| `OUTBOX_LEASE` | `30s` | Publisher claim lease |

Configuration is validated at startup. Production refuses the example `change-me` API key.

## Reliability guarantees

### Transactional ingestion

One PostgreSQL transaction inserts each new SMS, its outbox event, and the advanced ingestion checkpoint. The checkpoint uses the highest successfully persisted provider message ID. If any insert fails, the whole transaction rolls back and the checkpoint does not advance.

`ON CONFLICT (provider, provider_message_id) DO NOTHING` makes ingestion idempotent. A duplicate SMS does not create a duplicate outbox event.

### Outbox publishing

Publishers claim rows in a transaction using `FOR UPDATE SKIP LOCKED`, a lease, and the `processing` status. Multiple publisher instances are safe. Each JetStream message uses the outbox event UUID as `Nats-Msg-Id`, waits for acknowledgement, and only then marks the row `published`.

Failed publishes increment attempts and receive exponential backoff with jitter. Exhausted events become `failed` and are logged. An expired processing lease can be reclaimed, while stable message IDs suppress duplicate JetStream messages.

### Worker acknowledgement

The worker uses a durable pull consumer with explicit acknowledgement. It ACKs only after:

- the PostgreSQL processing transaction commits, or
- the dead letter database transaction commits and the DLQ JetStream publish is acknowledged.

Transient processing failures use delayed NAK. Permanent validation failures and exhausted delivery attempts go through DLQ persistence and publication. If DLQ persistence or publication fails, the original message is never ACKed.

Processing writes are idempotent: `processed_events.event_id` is unique, and SMS processing uses an upsert keyed by `(provider, provider_message_id)`.

### Graceful shutdown

All processes handle `SIGINT` and `SIGTERM`. Workers stop fetching before waiting for in-flight processing. The API stops accepting new requests and waits for active handlers during shutdown.

## HTTP API

All `/api/v1` routes require `X-API-Key` or `Authorization: Bearer <key>`.

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/health` | Liveness |
| `GET` | `/ready` | PostgreSQL and NATS readiness |
| `GET` | `/api/v1/messages` | Paginated messages, sender/date/message ID filters |
| `GET` | `/api/v1/messages/{id}` | One message |
| `GET` | `/api/v1/stats` | Total, processed, outbox, failed outbox, DLQ counts |
| `GET` | `/api/v1/outbox` | Paginated outbox events with status filter |
| `GET` | `/api/v1/outbox/{id}` | One outbox event |
| `GET` | `/api/v1/dlq` | Paginated dead letters |
| `GET` | `/api/v1/dlq/{id}` | One dead letter |
| `POST` | `/api/v1/dlq/{id}/replay` | Queue a new replay event |
| `POST` | `/api/v1/dlq/{id}/resolve` | Resolve a dead letter |
| `GET` | `/api/v1/metrics` | Runtime, message, outbox, and DLQ metrics |

Message filters:

```text
page, page_size, sender, from, to, message_id
```

`from` and `to` accept RFC3339 or `YYYY-MM-DD`. Pagination is capped at 100 rows per page.

Examples:

```sh
curl -H 'X-API-Key: change-me' \
  'http://localhost:8080/api/v1/messages?sender=%2B254700000001&from=2026-01-01&to=2026-12-31'

curl -H 'X-API-Key: change-me' \
  'http://localhost:8080/api/v1/outbox?status=failed&page=1&page_size=20'

curl -H 'X-API-Key: change-me' \
  'http://localhost:8080/api/v1/dlq?status=unresolved'
```

### DLQ replay

Replay is transactional. OSYN creates a new outbox event with a new UUID and sets `replay_of` to the original event ID. The new event is published to `sms.incoming`. Replay count, last replay time, and last replay error are recorded. Resolved dead letters cannot be replayed. Failed messages are never silently deleted.

```sh
curl -X POST -H 'X-API-Key: change-me' \
  http://localhost:8080/api/v1/dlq/<dead-letter-uuid>/replay

curl -X POST -H 'X-API-Key: change-me' \
  http://localhost:8080/api/v1/dlq/<dead-letter-uuid>/resolve
```

## Structured accident reports

Only SMS messages starting with `OSY` are accepted as accident reports. The message format is:

```text
OSY|lat=36.7538|lon=3.0588|damage=75|radius=500
```

Accepted aliases:

- Latitude: `lat`, `latitude`
- Longitude: `lon`, `lng`, `longitude`
- Damage: `damage`, `damage_percentage`, `damage_percent`
- Radius: `radius`, `range`, `radius_meters`

Examples:

```text
OSY|lat=36.7538|lon=3.0588|damage=75%|radius=1.2km
OSY;latitude=35.6971;longitude=-0.6308;damage=40;range=850m
```

Validation rules:

- Latitude must be between `-90` and `90`.
- Longitude must be between `-180` and `180`.
- Damage percentage must be between `0` and `100`.
- Radius must be between `1` meter and `ACCIDENT_MAX_RADIUS_METERS`.
- Invalid reports are routed to the existing DLQ flow and are not deleted.

The worker persists valid reports in `accident_reports`. It creates one emergency notification per configured recipient in `emergency_notifications`, then dispatches them through four concurrent notification workers. Notification writes are idempotent by `(accident_report_id, service)`.

Recipients:

| Service | Default number | Arabic role |
| --- | --- | --- |
| Civil Protection | `14` | Injuries, ambulance, fire, rescue |
| National Gendarmerie | `1055` | Incidents outside urban police jurisdiction and road incidents |
| National Police | `1548` | Incidents inside cities and urban areas |

Each recipient receives a different formal Arabic message. Example:

```text
إلى الحماية المدنية:

نحن منصة OSYN، تلقّينا بلاغاً عن حادث.
مرجع البلاغ: 990001
خط العرض: 36.753800
خط الطول: 3.058800
نسبة الأضرار التقديرية: 75.0%
النطاق التقريبي: 500 متر
الدور المطلوب: الاستجابة للإصابات والحرائق والإنقاذ.

يرجى التحقق والتدخل العاجل.
هذه رسالة آلية من منصة OSYN.
```

### Twilio delivery

Dry-run is enabled by default. It logs and stores the exact Arabic messages without sending them.

Live delivery requires:

- A Twilio sender number or Messaging Service SID.
- `TWILIO_ACCOUNT_SID` and `TWILIO_AUTH_TOKEN`.
- `TWILIO_DRY_RUN=false`.
- E.164 recipients in `EMERGENCY_*_PHONE`.

Twilio does not accept local short emergency numbers such as `14`, `1055`, or `1548` as `To` values. For live delivery, configure the real E.164 routing numbers supplied by the receiving agencies. Never use the emergency numbers as a test destination.

```env
TWILIO_DRY_RUN=false
TWILIO_ACCOUNT_SID=AC...
TWILIO_AUTH_TOKEN=...
TWILIO_FROM_NUMBER=+1...
EMERGENCY_CIVIL_PROTECTION_PHONE=+213...
EMERGENCY_GENDARMERIE_PHONE=+213...
EMERGENCY_POLICE_PHONE=+213...
```

### WhatsApp operator confirmation

The emergency numbers can be kept as placeholders while WhatsApp is used for test confirmation. Enable the Twilio WhatsApp Sandbox and set:

```env
WHATSAPP_NOTIFICATIONS_ENABLED=true
TWILIO_WHATSAPP_FROM=whatsapp:+17372508034
OPERATOR_WHATSAPP_NUMBER=+213794591246
WHATSAPP_DRY_RUN=false
```

The worker still creates the three placeholder emergency notifications and adds a fourth notification with service `operator_whatsapp`. The WhatsApp message contains the accident reference, coordinates, damage, radius, and the status of each emergency recipient.

Before Twilio can deliver a sandbox WhatsApp message, send `join twilio-trial` from the destination WhatsApp account to `+1 737 250 8034`. Twilio must confirm that the device is linked. WhatsApp business-initiated messages require an approved template outside an open conversation window.

The default emergency recipients remain labels unless an authorized local SMS gateway is connected. The WhatsApp summary explicitly says the emergency notifications are experimental, so it never claims a real emergency service received an alert.

### Test the accident pipeline

The safest end-to-end test is to publish a structured event to NATS:

```sh
docker run --rm --network osyn_default natsio/nats-box:latest \
  nats pub -s nats://nats:4222 sms.incoming \
  '{"event_id":"accident-test-001","provider":"africastalking","provider_message_id":"990001","sender":"+213557708655","recipient":"19210","message":"OSY|lat=36.7538|lon=3.0588|damage=75|radius=500","received_at":"2026-10-30T16:00:00Z"}'
```

Inspect results:

```sh
docker compose logs -f worker

docker compose exec postgres psql -U osyn -d osyn -c \
  "SELECT * FROM accident_reports ORDER BY id DESC;"

docker compose exec postgres psql -U osyn -d osyn -c \
  "SELECT service, target_phone, status, attempts, twilio_sid FROM emergency_notifications ORDER BY id;"
```

For an invalid report test:

```sh
docker run --rm --network osyn_default natsio/nats-box:latest \
  nats pub -s nats://nats:4222 sms.incoming \
  '{"event_id":"accident-invalid-001","provider":"africastalking","provider_message_id":"990002","sender":"+213557708655","recipient":"19210","message":"OSY|lat=999|lon=3.0588|damage=150|radius=0","received_at":"2026-10-30T16:01:00Z"}'
```

The invalid event appears in `/api/v1/dlq`.

## Database

SQL migrations live in `db/migrations` and are applied automatically, under an advisory lock, by every process at startup. Migrations are tracked in `schema_migrations`.

Core tables:

- `sms_messages`
- `ingestion_checkpoints`
- `outbox_events`
- `processed_events`
- `dead_letters`
- `accident_reports`
- `emergency_notifications`

Inspect data:

```sh
make psql

select id, provider_message_id, sender, processing_status from sms_messages order by id;
select id, status, attempts, subject from outbox_events order by created_at desc;
select id, event_id, status, replay_count, error from dead_letters order by created_at desc;
```

## Local development

Start only the infrastructure:

```sh
docker compose up -d postgres nats
```

Point local connections at host ports:

```sh
export DATABASE_URL='postgres://osyn:osyn@localhost:5432/osyn?sslmode=disable'
export NATS_URL='nats://localhost:4222'
export API_KEY='change-me'
export AFRICASTALKING_USERNAME='sandbox'
export AFRICASTALKING_API_KEY='your-api-key'
```

Run processes in separate terminals:

```sh
go run ./cmd/api
go run ./cmd/fetcher
go run ./cmd/publisher
go run ./cmd/worker
```

Every process can be started independently and is safe to run as multiple replicas. PostgreSQL row locks, unique constraints, ingestion checkpoints, and JetStream durable consumers coordinate concurrent instances.

## Development commands

```sh
make build
make fmt
make vet
make check
make docker-build
make up
make logs
make ps
make seed
make down
```

Validate the OpenAPI file:

```sh
make swagger-check
```

## Operations

- Logs are structured JSON through `log/slog`.
- `/ready` returns `503` when PostgreSQL or NATS is unavailable.
- `/api/v1/metrics` includes process request counters, uptime, goroutines, message status counts, outbox status counts, and DLQ counts.
- Publisher failures are retried with jitter and logged when exhausted.
- Workers log retries, DLQ routing, and exhausted deliveries.
- No failed message is discarded.

## Security notes

- Never commit `.env` or real Africa's Talking credentials.
- Replace `API_KEY`, PostgreSQL credentials, and exposed ports before production use.
- Put the API behind TLS at the edge.
- Restrict NATS and PostgreSQL network access to trusted services.
