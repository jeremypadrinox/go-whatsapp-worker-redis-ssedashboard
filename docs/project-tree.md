# Go-WhatsApp Worker Project Tree

```
gowhatsapp-worker/
├── .air.toml
├── .env
├── .env.example
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
├── main.go
├── README.md
├── cmd/
│   ├── redis_worker.go
│   ├── root.go
│   └── start.go
├── docs/
│   ├── endpoint.md
│   ├── openapi.yml
│   ├── project-tree.md
│   └── system-documentation.html
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── database/
│   │   ├── direct_postgres.go
│   │   └── interface.go
│   ├── models/
│   │   └── message.go
│   ├── redis/
│   │   └── client.go
│   ├── server/
│   │   ├── auth.go
│   │   └── server.go
│   ├── services/
│   │   └── message_service.go
│   ├── utils/
│   │   └── file_storage.go
│   ├── whatsapp/
│   │   └── client.go
│   └── worker/
│       ├── health_monitor.go
│       ├── processor.go
│       └── rate_limiter.go
├── public/
│   ├── 1.png
│   ├── 2.png
│   ├── 3.png
│   └── 4.png
├── test/
│   ├── send_random_messages.py
│   └── signature.png
└── tmp/
	├── build-errors.log
	└── … (ephemeral runtime artefacts)
```

## Project Structure Explanation

### Root Level Files

| File/Folder | Type | Purpose | Key Features |
|-------------|------|---------|--------------|
| `main.go` | Entry Point | Application bootstrap | Cobra CLI integration |
| `go.mod` | Go Module | Module definition | Dependency graph, Go version |
| `go.sum` | Go Module | Dependency checksums | Supply-chain verification |
| `Dockerfile` | Container | Docker build recipe | Multi-stage build, production image |
| `docker-compose.yml` | Container | Local stack orchestration | Spins up API, worker, PostgreSQL, Redis |
| `.air.toml` | Dev Tooling | Hot reload configuration | `air` watcher settings |
| `.env`, `.env.example` | Environment | Runtime configuration | Connection strings, feature flags |
| `README.md` | Documentation | Project overview | Usage, deployment, troubleshooting |

### Command Line Applications (`cmd/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `cmd/redis_worker.go` | Redis Worker | Standalone queue consumer |
| `cmd/root.go` | Root Command | Shared flags, configuration wiring |
| `cmd/start.go` | Main Start | Runs API server, worker pool, health monitor |

### Internal Package Structure

#### Configuration Layer (`internal/config/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `config.go` | Configuration Management | Environment variables, validation, defaults |

#### Database Layer (`internal/database/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `interface.go` | Database Interface | Abstract database operations |
| `direct_postgres.go` | PostgreSQL Implementation | Direct DB connection, connection pooling |


#### Models Layer (`internal/models/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `message.go` | Data Models | Database models, validation methods |

#### Redis Layer (`internal/redis/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `client.go` | Redis Client | Queue operations, connection pooling |

#### Server Layer (`internal/server/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `auth.go` | Authentication | Basic Auth middleware |
| `server.go` | HTTP Server | Web dashboard, REST API, SSE streaming |

#### Services Layer (`internal/services/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `message_service.go` | Message Service | High-level orchestration, payload validation, queuing |

#### Utility Layer (`internal/utils/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `file_storage.go` | Temporary Storage Abstraction | Persists uploads to `tmp/`, secure path validation, deletion helpers |

#### WhatsApp Integration (`internal/whatsapp/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `client.go` | WhatsApp Client | API communication, typing simulation |

#### Worker Implementation (`internal/worker/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `health_monitor.go` | Health Monitoring | Automated health checks, cron jobs |
| `processor.go` | Message Processor | Core engine, retries, per-message cleanup, hourly tmp purge, Redis stream processing |
| `rate_limiter.go` | Rate Limiting | Message rate control, natural behavior, bulk vs manual profiles |

### Documentation (`docs/`)

| File | Purpose | Key Features |
|------|---------|--------------|
| `project-tree.md` | Project Structure | Complete project tree and explanations |
| `endpoint.md` | API Documentation | REST API endpoints, usage examples |
| `openapi.yml` | API Schema | Machine-readable contract for REST API |
| `system-documentation.html` | System Overview | Rich, navigable documentation portal |

## Architecture Overview

### Clean Architecture Layers

| Layer | Purpose | Components |
|-------|---------|------------|
| **Domain** | Business Logic | `models/` |
| **Application** | Application Logic | `services/` |
| **Infrastructure** | External Concerns | `database/`, `redis/`, `whatsapp/` |
| **Interface** | User Interface | `server/`, `cmd/` |

### Key Features

| Feature | Implementation | Benefits |
|---------|----------------|----------|
| **Concurrent Processing** | Worker pools, goroutines | High throughput |
| **Rate Limiting** | Configurable delays, natural breaks | API compliance |
| **Health Monitoring** | Automated checks, recovery | System reliability |
| **Real-time Dashboard** | SSE streaming, live updates | Operational visibility |
| **Queue Management** | Redis-based queuing | Scalability, reliability |
| **Authentication** | Multiple auth types | Security flexibility |
| **File Lifecycle Management** | Immediate cleanup on success/failure + hourly purge | Prevents tmp/ bloat |

### Performance Characteristics

| Component | Optimization | Impact |
|-----------|--------------|--------|
| **Database** | Connection pooling (25 max) | Reduced connection overhead |
| **Redis** | Blocking operations | Real-time processing |
| **HTTP Server** | SSE streaming | Live dashboard updates |
| **Worker** | Concurrent processing | High message throughput |
| **Rate Limiter** | Natural behavior simulation | API limit compliance |
