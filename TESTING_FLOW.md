# UDBP Testing Flow - Complete Guide

This document provides a comprehensive testing flow for the Universal Database Proxy (UDBP), covering all setup, execution, and verification steps.

---

## Table of Contents
1. [Prerequisites](#prerequisites)
2. [Environment Setup](#environment-setup)
3. [Starting the Backend Server](#starting-the-backend-server)
4. [Testing Database Connections](#testing-database-connections)
5. [Testing API Endpoints](#testing-api-endpoints)
6. [Testing Smart Engines](#testing-smart-engines)
7. [Testing Query Routing](#testing-query-routing)
8. [Testing Security Features](#testing-security-features)
9. [Testing Frontend](#testing-frontend)
10. [Stopping the Servers](#stopping-the-servers)

---

## 1. Prerequisites

Before testing, ensure you have the following installed:

| Requirement | Version | Purpose |
|-------------|---------|---------|
| Go | 1.19+ | Backend runtime |
| Node.js | 18+ | Frontend runtime |
| PostgreSQL | 15+ | Primary database |
| MySQL | 8.0+ | Alternative database |
| Redis | 7+ | Cache layer |
| MongoDB | 7+ | Document database |
| Docker | Latest | Container runtime |
| Docker Compose | Latest | Orchestration |

### Verify Installations

```bash
# Check Go version
go version

# Check Node.js version
node --version

# Check Docker version
docker --version

# Check Docker Compose version
docker-compose --version
```

---

## 2. Environment Setup

### Option A: Using Docker Compose (Recommended)

```bash
# Navigate to project directory
cd "Python/Opencode/Database Proxy Tool"

# Start all services (databases + backend + frontend)
docker-compose up -d

# Verify all services are running
docker-compose ps
```

Expected output:
```
         Name                        Command               State    Ports
--------------------------------------------------------------------------------
udbproxy-backend        ./udbproxy                    Up      5432, 8080, 9090
udbproxy-frontend       npm run dev                   Up      5173
udbproxy-mysql          docker-entrypoint.sh          Up      3306
udbproxy-mongodb        mongod                        Up      27017
udbproxy-postgres-pri   postgres                      Up      5432
udbproxy-postgres-rep   postgres                      Up      5433
udbproxy-redis          redis-server                  Up      6379
```

### Option B: Manual Setup (Without Docker)

```bash
# Start required databases manually
# PostgreSQL
pg_ctl -D /usr/local/var/postgres start

# MySQL
mysql.server start

# Redis
redis-server

# MongoDB
mongod
```

---

## 3. Starting the Backend Server

### Using `go run` (Development)

```bash
# Navigate to backend directory
cd "Python/Opencode/Database Proxy Tool/backend"

# Run the server with default config
go run ./cmd/udbproxy/main.go serve

# Or with custom config
go run ./cmd/udbproxy/main.go serve --config /path/to/config.yaml

# Or with debug logging
go run ./cmd/udbproxy/main.go serve --debug
```

### Using Built Binary

```bash
# Navigate to backend directory
cd "Python/Opencode/Database Proxy Tool/backend"

# Build the application
go build -o udbproxy ./cmd/udbproxy

# Start the server
./udbproxy serve

# Or with custom config
./udbproxy serve --config config.yaml

# Or with debug logging
./udbproxy serve --debug
```

### Verify Server is Running

```bash
# Check health endpoint
curl http://localhost:8080/health

# Expected response:
# {"status": "healthy", "uptime": 12345}
```

---

## 4. Testing Database Connections

### 4.1 Test PostgreSQL Connection

```bash
# Connect via psql
psql -h localhost -p 5432 -U postgres -d myapp

# Test query
SELECT 1 as test;
```

### 4.2 Test MySQL Connection

```bash
# Connect via mysql
mysql -h localhost -P 3306 -u root -p myapp

# Test query
SELECT 1 AS test;
```

### 4.3 Test Redis Connection

```bash
# Connect via redis-cli
redis-cli -h localhost -p 6379

# Test ping
PING

# Expected: PONG
```

### 4.4 Test MongoDB Connection

```bash
# Connect via mongosh
mongosh mongodb://localhost:27017/myapp

# Test ping
db.runCommand({ ping: 1 })

# Expected: { ok: 1 }
```

### 4.5 Test via UDBP API

```bash
# List all configured databases
curl http://localhost:8080/api/v1/databases

# Test specific database connection
curl -X POST http://localhost:8080/api/v1/databases/postgres-primary/test

# Expected response:
# {"success": true, "latency": 5}
```

---

## 5. Testing API Endpoints

### 5.1 Health & Status

```bash
# Check server health
curl http://localhost:8080/health

# Get overall statistics
curl http://localhost:8080/api/v1/stats
```

### 5.2 Engine Management

```bash
# List all engines
curl http://localhost:8080/api/v1/engines

# Get engine details
curl http://localhost:8080/api/v1/engines/cache

# Enable an engine
curl -X POST http://localhost:8080/api/v1/engines/cache/enable

# Disable an engine
curl -X POST http://localhost:8080/api/v1/engines/cache/disable

# Get engine statistics
curl http://localhost:8080/api/v1/engines/cache/stats
```

### 5.3 Database Management

```bash
# List all databases
curl http://localhost:8080/api/v1/databases

# Get database details
curl http://localhost:8080/api/v1/databases/primary

# Add new database
curl -X POST http://localhost:8080/api/v1/databases \
  -H "Content-Type: application/json" \
  -d '{"name": "test-db", "type": "postgres", "host": "localhost", "port": 5432}'

# Remove database
curl -X DELETE http://localhost:8080/api/v1/databases/test-db
```

### 5.4 Query Operations

```bash
# Get query history
curl http://localhost:8080/api/v1/query/history

# Execute query (via API)
curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -d '{"query": "SELECT 1 as test", "database": "primary"}'
```

---

## 6. Testing Smart Engines

### 6.1 Test Cache Engine

```bash
# Enable cache engine
curl -X POST http://localhost:8080/api/v1/engines/cache/enable

# Execute same query twice - second should be faster
time curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -d '{"query": "SELECT * FROM users", "database": "primary"}'

time curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -d '{"query": "SELECT * FROM users", "database": "primary"}'
```

### 6.2 Test Security Engine

```bash
# Test SQL injection detection - should be blocked
curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -d '{"query": "SELECT * FROM users WHERE id = 1; DROP TABLE users;", "database": "primary"}'

# Expected: {"error": "SQL injection detected"}
```

### 6.3 Test Query Rewrite Engine

```bash
# Enable query rewrite engine
curl -X POST http://localhost:8080/api/v1/engines/query_rewrite/enable

# Execute a query that should be rewritten
curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -d '{"query": "SELECT * FROM orders WHERE id IN (SELECT id FROM active_orders)", "database": "primary"}'

# Check logs for rewrite information
curl http://localhost:8080/api/v1/query/history
```

### 6.4 Test Rate Limiting Engine

```bash
# Enable rate limiting
curl -X POST http://localhost:8080/api/v1/engines/ratelimit/enable

# Send many requests rapidly
for i in {1..100}; do
  curl -s http://localhost:8080/health > /dev/null
done

# Some requests should be rate limited
# Check response for 429 Too Many Requests
```

---

## 7. Testing Query Routing

### 7.1 Test Read/Write Splitting

```bash
# SELECT queries should route to replica
curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -d '{"query": "SELECT * FROM users", "database": "primary"}'

# INSERT/UPDATE/DELETE should route to primary
curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -d '{"query": "INSERT INTO users (name) VALUES (\"test\")", "database": "primary"}'
```

### 7.2 Test Load Balancing

```bash
# Multiple SELECT queries should be distributed across replicas
for i in {1..10}; do
  curl -X POST http://localhost:8080/api/v1/query/execute \
    -H "Content-Type: application/json" \
    -d '{"query": "SELECT * FROM users LIMIT 1", "database": "replica"}'
done

# Check load balancer statistics
curl http://localhost:8080/api/v1/engines/load_balancer/stats
```

---

## 8. Testing Security Features

### 8.1 Test RBAC

```bash
# Test with unauthorized user - should be denied
curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer user-token" \
  -d '{"query": "DROP TABLE users", "database": "primary"}'

# Expected: {"error": "Permission denied"}
```

### 8.2 Test Data Masking

```bash
# Query with sensitive data - should be masked
curl -X POST http://localhost:8080/api/v1/query/execute \
  -H "Content-Type: application/json" \
  -d '{"query": "SELECT email, password FROM users", "database": "primary"}'

# Expected: email and password fields should be masked
```

### 8.3 Test mTLS

```bash
# If TLS is enabled, test with client certificate
curl --cert client.crt --key client.key \
  https://localhost:5432/health

# Without certificate - should fail
curl https://localhost:5432/health

# Expected: SSL certificate error
```

---

## 9. Testing Frontend

### 9.1 Start Frontend Development Server

```bash
# Navigate to frontend directory
cd "Python/Opencode/Database Proxy Tool/frontend"

# Install dependencies
npm install

# Start development server
npm run dev
```

### 9.2 Access Frontend

Open browser to: `http://localhost:5173`

### 9.3 Test Frontend Pages

| Page | URL | Test |
|------|-----|------|
| Dashboard | `/` | Verify statistics display |
| Engines | `/engines` | List and toggle engines |
| Databases | `/databases` | Add/remove databases |
| Statistics | `/stats` | View performance metrics |
| Settings | `/settings` | Modify configuration |

### 9.4 Test API Integration

```bash
# Verify frontend can connect to backend
curl http://localhost:8080/api/v1/stats

# Check CORS headers
curl -I http://localhost:8080/api/v1/engines
```

---

## 10. Stopping the Servers

### Docker Compose

```bash
# Stop all services
docker-compose down

# Stop and remove volumes (data will be lost)
docker-compose down -v
```

### Manual (Without Docker)

```bash
# Stop backend server (Ctrl+C in terminal)
# Or kill the process
pkill udbproxy

# Stop frontend (Ctrl+C in terminal)
# Or kill the process
pkill -f "vite"

# Stop databases
pg_ctl -D /usr/local/var/postgres stop
mysql.server stop
redis-cli shutdown
mongosh --eval "db.adminCommand({ shutdown: 1 })"
```

### Verify All Services Stopped

```bash
# Check running processes
docker-compose ps

# Check ports
netstat -an | grep -E '(5432|3306|6379|27017|8080|5173)'
```

---

## Troubleshooting

### Common Issues

| Issue | Solution |
|-------|----------|
| Port already in use | Check and kill existing process: `lsof -i :5432` |
| Database connection failed | Verify database is running: `pg_isready` |
| Module not found | Run: `go mod download` |
| CORS errors | Check frontend API URL configuration |
| Permission denied | Check file permissions and SELinux |

### Check Logs

```bash
# Backend logs
docker-compose logs backend
# Or
tail -f /var/log/udbproxy.log

# Database logs
docker-compose logs postgres-primary

# Frontend logs
docker-compose logs frontend
```

---

## Summary

This testing flow covers:

1. ✅ Environment setup (Docker/Manual)
2. ✅ Backend server startup
3. ✅ Database connectivity tests
4. ✅ API endpoint tests
5. ✅ Smart engine tests
6. ✅ Query routing tests
7. ✅ Security feature tests
8. ✅ Frontend integration tests
9. ✅ Proper shutdown procedures

For more information, see [END-to-END-FLOW.md](./END-to-END-FLOW.md) and [Database_proxy.md](./Database_proxy.md).
