# API Endpoints

_Last updated: 26 Sep 2025_

This document lists HTTP endpoints available in the Go-WhatsApp Worker project, their HTTP methods, the handler function, where they are registered, whether they are protected by authentication (auth middleware), and the expected request parameters/payloads.

Notes:
- The unified `Server` in `internal/server/server.go` is the main entrypoint handling the dashboard, REST API, SSE streams, and documentation endpoints.
- All requests are protected by Basic Authentication when `ENABLE_AUTH=true`; credentials come from `DASHBOARD_USERNAME` / `DASHBOARD_PASSWORD`.
- Every send request is queued in Redis Streams and processed asynchronously before calling Aldi Kemal's [`go-whatsapp-web-multidevice`](https://github.com/aldinokemal/go-whatsapp-web-multidevice).

---

## Endpoints

1. GET /
- Handler: `Server.handleMainInterface`
- Registered in: `internal/server/server.go` (setupRoutes)
- Auth: protected by auth middleware when `EnableAuth` is true
- Parameters: none (serves HTML dashboard with Message Playground)

2. GET /dashboard
- Handler: `Server.handleDashboard` (redirects to `/`)
- Registered in: `internal/server/server.go`
- Auth: protected
- Parameters: none

3. (Removed) /health endpoint is not available.

4. POST /api/send/message
- Handler: `Server.handleAPISendMessage`
- Registered in: `internal/server/server.go`
- Auth: protected
- Content-Type: application/json
- Request JSON body shape (WhatsAppMessageRequest):
  - phone (string) required - Phone number with country code (supports comma-separated for bulk: "1234567890,0987654321")
  - message (string) required - Message to send
  - reply_message_id (string) optional - Message ID to reply to
  - is_forwarded (boolean) optional - Whether this is a forwarded message
  - duration (integer) optional - Disappearing message duration in seconds
- Response: SendMessageResponse { success, message, data: { message_id, status }, error }
- **Bulk Support**: Comma-separated `phone` values trigger the bulk engine. Messages are split into Redis Stream entries, persisted with chunk metadata, and the response `data.message_id` returns the campaign token.

5. POST /api/send/image
- Handler: `Server.handleAPISendImage`
- Registered in: `internal/server/server.go`
- Auth: protected
- Content-Type: multipart/form-data
- Request form data:
  - phone (string) required - Phone number with country code (supports comma-separated for bulk: "1234567890,0987654321")
  - caption (string) optional - Caption to send
  - view_once (boolean) optional - View once
  - image (file) optional - Image file to send (stored in tmp/ directory)
  - image_url (string) optional - Image URL to send
  - compress (boolean) optional - Compress image
  - duration (integer) optional - Disappearing message duration in seconds
  - is_forwarded (boolean) optional - Whether this is a forwarded message
- Response: SendMessageResponse { success, message, data: { message_id, status }, error }
- **Bulk Support**: Automatically detects comma-separated phone numbers and queues individual messages for each recipient
- **File Storage**: Uploaded media is stored in the `tmp/` directory, removed immediately after successful delivery or final failure, and purged by an hourly background cleanup job

6. POST /api/send/file
- Handler: `Server.handleAPISendFile`
- Registered in: `internal/server/server.go`
- Auth: protected
- Content-Type: multipart/form-data
- Request form data:
  - phone (string) required - Phone number with country code (supports comma-separated for bulk: "1234567890,0987654321")
  - caption (string) optional - Caption to send
  - file (file) optional - File to send (stored in tmp/ directory)
  - is_forwarded (boolean) optional - Whether this is a forwarded message
  - duration (integer) optional - Disappearing message duration in seconds
- Response: SendMessageResponse { success, message, data: { message_id, status }, error }
- **Bulk Support**: Automatically detects comma-separated phone numbers and queues individual messages for each recipient
- **File Storage**: Uploaded documents are stored in the `tmp/` directory, removed immediately after successful delivery or final failure, and purged by an hourly background cleanup job

7. GET /api/statistics/stream
- Handler: `Server.handleAPIStatisticsStream`
- Registered in: `internal/server/server.go`
- Auth: protected
- Description: Server-Sent Events (SSE) stream providing periodic JSON payloads with worker, database, and Redis stats. Content-Type: text/event-stream

8. GET /docs
- Handler: `Server.handleDocumentation`
- Registered in: `internal/server/server.go`
- Auth: protected by auth middleware when `EnableAuth` is true
- Returns: Comprehensive system documentation HTML page with detailed information about architecture, components, API endpoints, configuration, and performance characteristics

9. GET /docs/openapi.yml
- Handler: `Server.handleOpenAPISpec`
- Registered in: `internal/server/server.go`
- Auth: protected
- Returns: Raw OpenAPI 3.0 YAML specification. Served with no-cache headers for live updates.

11. GET /docs/swagger
- Handler: `Server.handleSwagger`
- Registered in: `internal/server/server.go`
- Auth: protected
- Returns: Embedded Swagger UI (served from CDN) pointing to `/docs/openapi.yml` for interactive exploration.

---

## Message Playground UI

The dashboard includes a Message Playground that provides a user-friendly interface for testing all three message types:

- **Send Message**: Text message sending with support for multiple recipients
- **Send Image**: Image sending with captions and various options
- **Send File**: File sending with descriptions

The UI automatically handles:
- Multiple recipient support (sends individual requests to each recipient)
- File uploads for images and files
- Form validation and error handling
- Real-time feedback and success/error notifications

---

## Integration with Aldi Kemal's WhatsApp API

This system is designed to work with [Aldi Kemal's go-whatsapp-web-multidevice](https://github.com/aldinokemal/go-whatsapp-web-multidevice) project:

- All API endpoints mirror the structure and parameters of the original WhatsApp API
- Messages are queued in Redis for reliable processing
- Workers handle rate limiting and natural delays
- Individual requests are sent to the WhatsApp API for each recipient
- Supports all message types: text, image, and file

---

Notes / Caveats
- All endpoints are registered in the unified server (`internal/server/server.go`) which applies Basic Auth middleware whenever authentication is enabled.
- Request contracts live in `internal/services/message_service.go` (`WhatsAppMessageRequest`, `WhatsAppMediaRequest`, `WhatsAppFileRequest`, plus their bulk counterparts).
- Bulk sends are expanded into individual Redis Stream entries; chunking, retries, and rate limiting happen in the worker (`internal/worker/processor.go`).
- Failed messages follow exponential backoff (5s, 10s, 20s, …) up to `WORKER_MAX_RETRIES`, after which they are marked failed and files are cleaned up.

---

## Configuration / Environment Variables

The application supports various environment variables for configuration. Key variables include:

### Rate Limiting & Pacing
- `RATE_PROFILE`: Message sending profile (`manual` for fast operator responses, `bulk` for slow human-like bulk campaigns). Default: `manual`
- `RATE_DELAY_MEAN_MS`: Mean delay between messages in milliseconds. Default: 10000 (10s for bulk)
- `RATE_DELAY_JITTER_MS`: Random jitter added to delay in milliseconds. Default: 5000 (5s)
- `RATE_BURST_LIMIT`: Number of messages before a cooldown break. Default: 20
- `RATE_COOLDOWN_SECONDS`: Cooldown duration in seconds after burst limit. Default: 210 (3.5 min)
- `BULK_CHUNK_SIZE`: Number of recipients per chunk for bulk campaigns. Default: 500
- `WORKER_CONCURRENCY`: Number of concurrent workers for bulk processing. Default: 10
- `WORKER_BATCH_SIZE`: Batch size for message processing. Default: 50

### Legacy Rate Settings (for manual profile)
- `RATE_LIMIT_DELAY_MIN/MAX`: Min/max delay between messages (e.g., "2s", "5s")
- `TYPING_DELAY_MIN/MAX`: Min/max typing simulation delay (e.g., "1s", "3s")
- `NATURAL_BREAK_INTERVAL`: Messages before break (e.g., 50)
- `NATURAL_BREAK_DURATION_MIN/MAX`: Break duration (e.g., "30s", "1m")

### Database & Redis
- `DATABASE_HOST/PORT/NAME/USER/PASSWORD/SSL_MODE`: PostgreSQL connection
- `REDIS_HOST/PORT/PASSWORD/DATABASE`: Redis connection
- `REDIS_QUEUE_NAME`: Redis stream name for message queue. Default: `pending_messages`
- `REDIS_CONSUMER_GROUP`: Consumer group name. Default: `workers`
- `REDIS_CONSUMER_NAME`: Consumer name. Default: `worker-1`
- `REDIS_STREAM_MAX_LEN`: Maximum stream length. Default: 100000

### API & Auth
- `API_HOST/PORT`: Server host/port
- `ENABLE_AUTH/AUTH_TYPE/DASHBOARD_USERNAME/PASSWORD`: Authentication settings

See `internal/config/config.go` for full list and defaults.

---

## Additional Notes
- Worker processes send typing presence signals to mimic natural user behaviour while dispatching messages.
- Redis-backed retry handling automatically marks messages as failed after max attempts, triggering file cleanup if media uploads were involved.

Last updated: 26 September 2025
