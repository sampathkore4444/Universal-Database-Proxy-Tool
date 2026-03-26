# Universal Database Proxy (UDBP) - End-to-End Flow Documentation

## Table of Contents
1. [Platform Overview](#platform-overview)
2. [Architecture Components](#architecture-components)
3. [End-to-End Data Flow](#end-to-end-data-flow)
4. [Smart Engines Detailed Explanation](#smart-engines-detailed-explanation)
5. [Frontend to Backend Communication](#frontend-to-backend-communication)
6. [API Endpoints Reference](#api-endpoints-reference)
7. [Starting and Stopping the Servers](#starting-and-stopping-the-servers)
8. [Configuration Guide](#configuration-guide)
9. [Database Support](#database-support)

---

## 1. Platform Overview

The **Universal Database Proxy (UDBP)** is a production-grade, high-performance database proxy layer designed to sit between applications and databases. It provides intelligent query routing, security, caching, observability, and 31+ smart engines for advanced database operations.

### Key Features
- **Multi-Database Support**: MySQL, PostgreSQL, MongoDB, Redis
- **31 Smart Engines**: Security, Observability, Caching, AI Optimization, etc.
- **Read/Write Splitting**: Automatic routing of queries to appropriate databases
- **SQL Injection Detection**: Real-time threat detection and blocking
- **Query Result Caching**: Redis-based caching for improved performance
- **Observability**: Prometheus metrics, health checks, and logging
- **Plugin System**: Extensible architecture for custom functionality

---

## 2. Architecture Components

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           UDBP Architecture                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐                   │
│  │  Frontend   │────▶│  Management │◀────│   CLI Tool  │                   │
│  │  (React)    │     │    API      │     │  (Cobra)    │                   │
│  └─────────────┘     └─────────────┘     └─────────────┘                   │
│         │                   │                     │                         │
│         │                   ▼                     │                         │
│         │           ┌─────────────┐               │                         │
│         │           │   Server    │               │                         │
│         │           │  (HTTP +    │               │                         │
│         │           │  Protocol)  │               │                         │
│         │           └─────────────┘               │                         │
│         │                   │                     │                         │
│         ▼                   ▼                     ▼                         │
│  ┌──────────────────────────────────────────────────────────────┐          │
│  │                    Request Handler                             │          │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │          │
│  │  │  Protocol   │  │   Routing   │  │  Engine Pipeline    │  │          │
│  │  │  Handlers   │  │   Router    │  │  (31 Smart Engines) │  │          │
│  │  │  (MySQL/    │  │             │  │                     │  │          │
│  │  │   PG/Mongo) │  │             │  │                     │  │          │
│  │  └─────────────┘  └─────────────┘  └─────────────────────┘  │          │
│  └──────────────────────────────────────────────────────────────┘          │
│                                    │                                         │
│                                    ▼                                         │
│  ┌──────────────────────────────────────────────────────────────┐          │
│  │                    Backend Databases                          │          │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐     │          │
│  │  │ Primary  │  │ Replica  │  │  Cache   │  │  Secret  │     │          │
│  │  │   DB     │  │    DB    │  │ (Redis)  │  │  Manager │     │          │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘     │          │
│  └──────────────────────────────────────────────────────────────┘          │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Core Components

| Component | Description |
|-----------|-------------|
| **Frontend (React)** | Web UI for monitoring and configuration |
| **Management API** | REST API for all operations |
| **CLI Tool** | Command-line interface using Cobra |
| **Server** | HTTP server handling both management API and protocol handlers |
| **Protocol Handlers** | Database-specific protocol implementations |
| **Routing Router** | Intelligent query routing based on rules |
| **Engine Pipeline** | Chain of 31 smart engines for query processing |
| **Secret Manager** | Secure credential storage |

---

## 3. End-to-End Data Flow

### Phase 1: Request Reception

```
Client Application
        │
        ▼
┌───────────────────┐
│ Protocol Handler  │  ◀── Detects database type (MySQL/PostgreSQL/MongoDB/Redis)
│  (MySQL/PG/Mongo) │
└───────────────────┘
        │
        ▼
```

The proxy listens on a configurable port (default: 5432) and accepts incoming database connections. The protocol handler detects the database type by examining the initial handshake packets and routes the connection to the appropriate handler.

### Phase 2: Query Context Creation

```
┌─────────────────────────────┐
│  QueryContext Creation      │
│  ─────────────────────      │
│  • ID: unique query ID      │
│  • RawQuery: SQL string     │
│  • Database: target DB     │
│  • User: client username   │
│  • ClientAddr: IP address  │
│  • Timestamp: request time │
│  • Metadata: context data  │
└─────────────────────────────┘
        │
        ▼
```

A `QueryContext` struct is created containing all information about the incoming query. This context is passed through the entire engine pipeline.

### Phase 3: Security Engine Processing

```
┌─────────────────────────────┐
│   Security Engine           │
│  ─────────────────────      │
│  • SQL Injection Detection  │
│  • RBAC Validation          │
│  • Query Blocking           │
│  • Sensitive Data Masking   │
└─────────────────────────────┘
        │
        ▼
```

The Security Engine (security_engine.go) performs:
- **SQL Injection Detection**: Uses regex patterns to identify malicious SQL
- **RBAC Validation**: Checks user permissions against access policies
- **Query Blocking**: Denies queries matching security rules
- **Data Masking**: Masks sensitive fields (email, phone, etc.)

### Phase 4: Routing Decision

```
┌─────────────────────────────┐
│   Routing Router            │
│  ─────────────────────      │
│  • Pattern Matching         │
│  • Read/Write Splitting     │
│  • Load Balancing           │
│  • Priority Resolution      │
└─────────────────────────────┘
        │
        ▼
```

The router (routing/router.go) determines:
1. **Which database** to route the query to
2. **Whether to use read replica** for SELECT queries
3. **Load balancing strategy** (round-robin, least connections, etc.)

Example routing rules from config.yaml:
```yaml
routing_rules:
  - name: select_reads
    match_pattern: "^SELECT"
    database: replica
    is_read_replica: true
    priority: 50
```

### Phase 5: Smart Engine Pipeline

```
┌────────────────────────────────────────────────────────────────────┐
│                    Engine Pipeline (31 Engines)                    │
├────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Query ──▶ Engine 1 ──▶ Engine 2 ──▶ ... ──▶ Engine N ──▶ Database │
│              │              │                    │                 │
│              ▼              ▼                    ▼                 │
│         Process()      Process()             Process()             │
│                                                                     │
│  Each engine can:                                                   │
│  • Modify the query                                                 │
│  • Add metadata                                                     │
│  • Block the query                                                  │
│  • Continue or stop the pipeline                                    │
│                                                                     │
└────────────────────────────────────────────────────────────────────┘
```

The engine pipeline processes queries through multiple engines in sequence. Each engine implements the `Engine` interface:

```go
type Engine interface {
    Name() string
    Process(ctx context.Context, qc *QueryContext) EngineResult
    ProcessResponse(ctx context.Context, qc *QueryContext) EngineResult
}
```

### Phase 6: Query Execution

```
┌─────────────────────────────┐
│   Database Connection Pool  │
│  ─────────────────────      │
│  • Connection Acquisition   │
│  • Query Execution          │
│  • Result Retrieval         │
│  • Connection Release       │
└─────────────────────────────┘
        │
        ▼
┌─────────────────────────────┐
│   Backend Database          │
│  (MySQL/PostgreSQL/MongoDB) │
└─────────────────────────────┘
```

The proxy maintains connection pools to backend databases for efficient resource usage.

### Phase 7: Response Processing

```
┌─────────────────────────────────────────┐
│         Response Processing             │
├─────────────────────────────────────────┤
│                                          │
│  Database Response                       │
│         │                                │
│         ▼                                │
│  Engine Pipeline (Reverse Order)         │
│         │                                │
│         ▼                                │
│  ┌─────────────┐  ┌─────────────┐       │
│  │   Cache     │  │   Audit     │       │
│  │   Engine    │  │   Engine    │       │
│  └─────────────┘  └─────────────┘       │
│         │                                │
│         ▼                                │
│  Response to Client                      │
│                                          │
└─────────────────────────────────────────┘
```

After query execution, the response flows back through the pipeline in reverse order, allowing engines to:
- Cache results
- Log audit trails
- Transform response data

---

## 4. Smart Engines Detailed Explanation

### Core Processing Engines

| Engine | File | Functionality |
|--------|------|---------------|
| **Query Rewrite** | query_rewrite_engine.go | Optimizes queries (subquery to JOIN, OR to IN, COUNT optimization) |
| **Query Translation** | query_translation_engine.go | Translates queries between database dialects |
| **Query Cost Estimator** | query_cost_estimator_engine.go | Estimates query complexity and provides recommendations |
| **Query History** | query_history_engine.go | Stores and retrieves query history |
| **Query Insights** | query_insights_engine.go | Analyzes query patterns and provides optimization suggestions |
| **Query Versioning** | query_versioning_engine.go | Manages query versions and changes |
| **Query Sandbox** | query_sandbox_engine.go | Tests queries in sandbox before execution |

### Data Management Engines

| Engine | File | Functionality |
|--------|------|---------------|
| **Data Lineage** | data_lineage_engine.go | Tracks data flow between tables |
| **Data Validation** | data_validation_engine.go | Validates query results against rules |
| **Data Compression** | data_compression_engine.go | Compresses data for storage/transfer |
| **CDC (Change Data Capture)** | cdc_engine.go | Captures database changes |
| **Time-Series** | timeseries_engine.go | Optimizes queries for time-series data |
| **Graph** | graph_engine.go | Handles graph database queries |

### Performance Engines

| Engine | File | Functionality |
|--------|------|---------------|
| **Cache** | cache_engine.go | Redis-based query result caching |
| **Connection Pool Optimizer** | connection_pool_optimizer_engine.go | Dynamic pool management |
| **Load Balancer** | load_balancer_engine.go | Distributes load across replicas |
| **Hotspot Detection** | hotspot_detection_engine.go | Identifies frequently accessed tables |
| **Batch Processing** | batch_processing_engine.go | Batches multiple queries |
| **AI Optimization** | ai_optimization_engine.go | Uses AI for query optimization |

### Reliability Engines

| Engine | File | Functionality |
|--------|------|---------------|
| **Failover** | failover_engine.go | Automatic failover to replicas |
| **Retry Intelligence** | retry_intelligence_engine.go | Smart retry with exponential backoff |
| **Rate Limit Intelligence** | ratelimit_intelligence_engine.go | Adaptive rate limiting |
| **Circuit Breaker** | ratelimit_intelligence_engine.go | Prevents cascade failures |

### Security Engines

| Engine | File | Functionality |
|--------|------|---------------|
| **Security** | security_engine.go | SQL injection, RBAC, access control |
| **Encryption** | encryption_engine.go | Transparent data encryption |
| **Multi-Tenant Isolation** | multitenant_isolation_engine.go | Tenant data isolation |

### Observability Engines

| Engine | File | Functionality |
|--------|------|---------------|
| **Audit** | audit_engine.go | Comprehensive audit logging |
| **Observability** | observability_engine.go | Metrics and tracing |
| **Schema Intelligence** | schema_intelligence_engine.go | Schema analysis and recommendations |

### Other Engines

| Engine | File | Functionality |
|--------|------|---------------|
| **Federation** | federation_engine.go | Cross-database queries |
| **Shadow Database** | shadow_database_engine.go | Traffic shadowing for testing |
| **Transformation** | transformation_engine.go | Data transformation pipeline |

---

## 5. Frontend to Backend Communication

### Technology Stack

- **Frontend**: React 18 with TypeScript, Material-UI (MUI)
- **Backend Communication**: Axios HTTP client
- **API Format**: RESTful JSON

### Communication Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Frontend Communication Flow                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  React Component                                                │
│       │                                                         │
│       ▼                                                         │
│  ┌─────────────────┐                                           │
│  │  API Service    │  (src/api/udbproxy.ts)                    │
│  │  - engineApi    │                                           │
│  │  - databaseApi  │                                           │
│  │  - statsApi     │                                           │
│  │  - healthApi    │                                           │
│  └─────────────────┘                                           │
│       │                                                         │
│       ▼                                                         │
│  ┌─────────────────┐                                           │
│  │  Axios Client   │  - Base URL: http://localhost:8080/api/v1 │
│  │                 │  - Timeout: 10000ms                       │
│  │                 │  - Headers: Content-Type: application/json│
│  └─────────────────┘                                           │
│       │                                                         │
│       ▼                                                         │
│  ┌─────────────────────────────────────────┐                   │
│  │         Backend Management API          │                   │
│  │         (HTTP Server on :8080)           │                   │
│  └─────────────────────────────────────────┘                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### API Service Implementation

The frontend uses a typed API service (src/api/udbproxy.ts):

```typescript
const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1';

const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
  },
});
```

### Key API Services

#### Engine API
```typescript
export const engineApi = {
  list: async (): Promise<Engine[]> => {
    const response = await api.get('/engines');
    return response.data;
  },
  enable: async (id: string): Promise<void> => {
    await api.post(`/engines/${id}/enable`);
  },
  disable: async (id: string): Promise<void> => {
    await api.post(`/engines/${id}/disable`);
  },
  getStats: async (id: string): Promise<EngineStats> => {
    const response = await api.get(`/engines/${id}/stats`);
    return response.data;
  },
};
```

#### Database API
```typescript
export const databaseApi = {
  list: async (): Promise<Database[]> => {
    const response = await api.get('/databases');
    return response.data;
  },
  add: async (db: Omit<Database, 'id'>): Promise<Database> => {
    const response = await api.post('/databases', db);
    return response.data;
  },
  test: async (id: string): Promise<{ success: boolean; latency: number }> => {
    const response = await api.post(`/databases/${id}/test`);
    return response.data;
  },
};
```

#### Stats API
```typescript
export const statsApi = {
  get: async (): Promise<Stats> => {
    const response = await api.get('/stats');
    return response.data;
  },
  getHistory: async (limit: number = 100): Promise<QueryRecord[]> => {
    const response = await api.get(`/query/history?limit=${limit}`);
    return response.data;
  },
};
```

### Frontend Pages

| Page | Route | Functionality |
|------|-------|---------------|
| Dashboard | `/` | Overview statistics, query counts, latency |
| Engines | `/engines` | List, enable/disable smart engines |
| Databases | `/databases` | Manage database connections |
| Statistics | `/stats` | Detailed performance metrics |
| Settings | `/settings` | Configuration options |

---

## 6. API Endpoints Reference

### Health & Status

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Server health check |
| GET | `/api/v1/stats` | Overall statistics |

### Engine Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/engines` | List all engines |
| GET | `/api/v1/engines/:id` | Get engine details |
| POST | `/api/v1/engines/:id/enable` | Enable engine |
| POST | `/api/v1/engines/:id/disable` | Disable engine |
| GET | `/api/v1/engines/:id/stats` | Get engine statistics |

### Database Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/databases` | List all databases |
| GET | `/api/v1/databases/:id` | Get database details |
| POST | `/api/v1/databases` | Add new database |
| DELETE | `/api/v1/databases/:id` | Remove database |
| POST | `/api/v1/databases/:id/test` | Test database connection |

### Query Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/query/history` | Get query history |
| POST | `/api/v1/query/execute` | Execute query |

---

## 7. Starting and Stopping the Servers

### Prerequisites

- Go 1.19+ for backend
- Node.js 18+ for frontend
- PostgreSQL, MySQL, MongoDB (as backend databases)
- Redis (for caching)

---

### Starting the Backend Server (Without Docker)

#### Method 1: Using `go run` (Recommended for Development)

```bash
# Navigate to the backend directory
cd Python/Opencode/Database\ Proxy\ Tool/backend

# Run the server directly with go run
go run ./cmd/udbproxy/main.go serve

# Or with custom config
go run ./cmd/udbproxy/main.go serve --config /path/to/config.yaml

# Or with debug logging
go run ./cmd/udbproxy/main.go serve --debug
```

#### Method 2: Build and Run

```bash
# Navigate to the backend directory
cd Python/Opencode/Database\ Proxy\ Tool/backend

# Build the application
go build -o udbproxy ./cmd/udbproxy

# Start the server with default config
./udbproxy serve

# Start with custom config
./udbproxy serve --config /path/to/config.yaml

# Start with debug logging
./udbproxy serve --debug
```

#### Method 3: Using Docker (Single Container)

```bash
# Navigate to the backend directory
cd Python/Opencode/Database\ Proxy\ Tool/backend

# Build the Docker image
docker build -t udbproxy:latest .

# Run the container
docker run -d \
  --name udbproxy \
  -p 5432:5432 \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -e DB_PASSWORD=your_password \
  -e MYSQL_PASSWORD=your_mysql_password \
  udbproxy:latest
```

#### Method 4: Using Kubernetes

```bash
# Apply the deployment
kubectl apply -f deploy/deployment.yaml

# Check pod status
kubectl get pods -l app=udbproxy

# View logs
kubectl logs -l app=udbproxy
```

### Starting the Frontend Development Server

```bash
# Navigate to frontend directory
cd Python/Opencode/Database Proxy Tool/frontend

# Install dependencies
npm install

# Start development server
npm run dev

# The frontend will be available at http://localhost:5173
```

### Starting with Docker Compose (Recommended)

Docker Compose provides the easiest way to start all services together:

```bash
# Navigate to project directory
cd Python/Opencode/Database\ Proxy\ Tool

# Start all services (databases + backend + frontend)
docker-compose up -d

# View logs
docker-compose logs -f

# Check service status
docker-compose ps
```

#### Services Started with Docker Compose:

| Service | Port | Description |
|---------|------|-------------|
| PostgreSQL Primary | 5432 | Main PostgreSQL database |
| PostgreSQL Replica | 5433 | Read replica for load balancing |
| MySQL | 3306 | MySQL database |
| Redis | 6379 | Cache layer |
| MongoDB | 27017 | Document database |
| Backend (UDBP) | 5432, 8080, 9090 | Go proxy server |
| Frontend | 5173 | React dev server |

#### Useful Docker Compose Commands:

```bash
# Start all services
docker-compose up -d

# Stop all services
docker-compose down

# Stop and remove volumes (data will be lost)
docker-compose down -v

# Rebuild services (after code changes)
docker-compose up -d --build

# View logs for specific service
docker-compose logs -f backend
docker-compose logs -f postgres-primary

# Restart specific service
docker-compose restart backend

# Access container shell
docker-compose exec backend sh
docker-compose exec postgres-primary psql -U postgres
```

### Building for Production

```bash
# Build frontend
cd frontend
npm run build

# The built files will be in frontend/dist
```

### Stopping the Servers

#### Stopping Docker Compose (Recommended)

```bash
# Stop all services
docker-compose down

# Stop and remove volumes
docker-compose down -v

# Stop specific service
docker-compose stop backend
```

#### Stopping the Backend (Without Docker)

```bash
# If running in foreground (Ctrl+C)
# The server will gracefully shutdown

# If running as a process
pkill udbproxy

# If running in single Docker container
docker stop udbproxy

# If running in Kubernetes
kubectl delete deployment udbproxy
```

#### Stopping the Frontend (Without Docker)

```bash
# If running in development (Ctrl+C)

# If running as a background process
pkill -f "vite"
```

### Health Check

```bash
# Check if server is running
curl http://localhost:8080/health

# Expected response:
# {"status": "healthy", "uptime": 12345}
```

### CLI Commands Reference

```bash
# Show version
./udbproxy version

# Check health
./udbproxy health

# List engines
./udbproxy engines list

# Enable an engine
./udbproxy engines enable <engine-name>

# Disable an engine
./udbproxy engines disable <engine-name>

# Get engine stats
./udbproxy engines stats <engine-name>

# List databases
./udbproxy databases list

# Add database
./udbproxy databases add <name>

# Remove database
./udbproxy databases remove <name>

# Show statistics
./udbproxy stats

# Reset statistics
./udbproxy stats reset
```

---

## 8. Configuration Guide

### Main Configuration File (config.yaml)

```yaml
server:
  listen_address: "0.0.0.0"
  listen_port: 5432
  read_timeout: 30
  write_timeout: 30
  idle_timeout: 120
  max_clients: 1000

databases:
  - name: primary
    type: postgres
    host: localhost
    port: 5432
    database: myapp
    username: postgres
    password: ${DB_PASSWORD}
    ssl_mode: false
    max_connections: 25
    max_idle_time: 300
    max_lifetime: 3600
    is_read_replica: false
    tags:
      env: production

  - name: replica
    type: postgres
    host: localhost
    port: 5433
    database: myapp
    username: postgres
    password: ${DB_PASSWORD}
    ssl_mode: false
    max_connections: 25
    max_idle_time: 300
    is_read_replica: true
    tags:
      env: production

routing_rules:
  - name: default
    match_pattern: ""
    database: primary
    priority: 100

  - name: select_reads
    match_pattern: "^SELECT"
    database: replica
    is_read_replica: true
    priority: 50

security_rules:
  - name: block_destructive
    match_pattern: "(DROP|DELETE|TRUNCATE)"
    action: DENY

  - name: log_sensitive
    match_pattern: "(password|credit_card|ssn)"
    action: LOG

  - name: mask_sensitive_data
    match_pattern: "(email|phone|address)"
    action: MASK
    mask_fields:
      - email
      - phone
      - address
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DB_PASSWORD` | Primary database password | - |
| `MYSQL_PASSWORD` | MySQL database password | - |
| `LOG_LEVEL` | Logging level (debug/info/warn/error) | info |
| `METRICS_PORT` | Prometheus metrics port | 9090 |

---

## 9. Database Support

### Supported Databases

| Database | Protocol Handler | Default Port |
|----------|-----------------|--------------|
| PostgreSQL | postgres_handler.go | 5432 |
| MySQL | mysql_handler.go | 3306 |
| MongoDB | mongodb_handler.go | 27017 |
| Redis | redis_handler.go | 6379 |

### Connection Pool Configuration

Each database connection can be configured with:
- `max_connections`: Maximum number of connections in the pool
- `max_idle_time`: Maximum time a connection can be idle
- `max_lifetime`: Maximum lifetime of a connection
- `is_read_replica`: Mark as read replica for load balancing

---

## Summary

The Universal Database Proxy provides a comprehensive solution for database management with:

1. **Unified Interface**: Single entry point for multiple databases
2. **Smart Processing**: 31+ engines for query optimization, security, and observability
3. **Flexible Configuration**: YAML-based configuration with environment variable support
4. **Easy Monitoring**: Web UI and REST API for real-time monitoring
5. **Production Ready**: Connection pooling, failover, rate limiting, and audit logging

For more details, refer to the main [Database_proxy.md](./Database_proxy.md) file.

