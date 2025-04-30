# Turso Schema Registry

A service for managing database schema migrations.

## Overview

The Schema Registry tracks database migration versions and their associated SQL scripts. This allows clients to:

1. Register new migrations
2. Fetch required migrations based on their current version
3. Verify if they're up-to-date with the latest schema version

## Typical Workflow

1. **Developer:** Register a new migration when making schema changes
2. **Client Application:**
   - On startup, call `/verify` with its current schema version
   - If updates are required, apply the returned migrations
   - Update its stored schema version

## API Reference

### 1. Register a New Migration

Add a new schema migration to the registry.

```bash
curl -X POST http://localhost:8080/migrations \
  -H "Content-Type: application/json" \
  -d '{
    "version": "1.0.0",
    "description": "Initial schema",
    "sql": "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
  }'
```

If you omit the `version` field, the service will auto-increment the patch version:

```bash
curl -X POST http://localhost:8080/migrations \
  -H "Content-Type: application/json" \
  -d '{
    "description": "Add timestamps to users",
    "sql": "ALTER TABLE users ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;"
  }'
```

Response:

```json
{
  "status": "migration-registered",
  "version": "1.0.0",
  "currentVersion": "1.0.0"
}
```

### 2. Get Current Schema Version

Retrieve the latest schema version.

```bash
curl http://localhost:8080/version
```

Response:

```json
{
  "currentVersion": "1.0.1"
}
```

### 3. Fetch Required Migrations

Retrieve migrations needed to update from a specific version.

```bash
curl "http://localhost:8080/migrations?from=0.0.0"
```

Response:

```json
{
  "requiredMigrations": [
    {
      "version": "1.0.0",
      "description": "Initial schema",
      "sql": "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);",
      "created_at": "2025-04-30T12:00:00Z"
    },
    {
      "version": "1.0.1",
      "description": "Add timestamps to users",
      "sql": "ALTER TABLE users ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;",
      "created_at": "2025-04-30T12:05:00Z"
    }
  ]
}
```

### 4. Verify Schema Version

Verify if a client's schema is up-to-date by sending the current version:

```bash
curl -X POST http://localhost:8080/verify \
  -H "Content-Type: application/json" \
  -d '{
    "version": "1.0.0"
  }'
```

Response if client needs to update:

```json
{
  "status": "update-required",
  "currentVersion": "1.0.1",
  "clientVersion": "1.0.0",
  "requiredMigrations": [
    {
      "version": "1.0.1",
      "description": "Add timestamps to users",
      "sql": "ALTER TABLE users ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;",
      "created_at": "2025-04-30T12:05:00Z"
    }
  ]
}
```

Response if client is up-to-date:

```json
{
  "status": "up-to-date",
  "currentVersion": "1.0.1",
  "clientVersion": "1.0.1"
}
```

### 5. Get Complete Schema Information

Retrieve the current version and all registered migrations.

```bash
curl http://localhost:8080/schema
```

Response:

```json
{
  "currentVersion": "1.0.1",
  "migrations": [
    {
      "version": "1.0.0",
      "description": "Initial schema",
      "sql": "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);",
      "created_at": "2025-04-30T12:00:00Z"
    },
    {
      "version": "1.0.1",
      "description": "Add timestamps to users",
      "sql": "ALTER TABLE users ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;",
      "created_at": "2025-04-30T12:05:00Z"
    }
  ]
}
```
