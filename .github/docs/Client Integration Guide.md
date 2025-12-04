# Client Integration Guide

## Overview

This guide covers the new server monitoring, metrics, and activity features added to the Server API. These changes enable real-time server health monitoring, historical metrics tracking, activity feeds, and dashboard statistics.

## For Agent Developers

### 1. Server Registration Changes

When registering a new server, you can now include additional metadata:

```bash
curl -X POST https://api.example.com/servers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "web-server-01",
    "platform": {
      "os": "linux",
      "arch": "amd64",
      "version": "Ubuntu 22.04"
    },
    "tags": {
      "environment": "production",
      "role": "web"
    },
    "location": "us-east-1",
    "ip_address": "192.168.1.100",
    "hostname": "web01.example.com"
  }'
```

**New Fields:**
- `location` (string, optional): Physical or cloud region location
- `ip_address` (string, optional): Server IP address
- `hostname` (string, optional): Server hostname

### 2. Sending Metrics (Agents)

Agents should periodically send metrics to keep the server status updated. **Recommended interval: 30-60 seconds.**

```bash
# Send metrics update
curl -X PUT https://api.example.com/servers/{server_id}/metrics \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "cpu_usage": 45.2,
    "memory_usage": 68.5,
    "disk_usage": 72.1
  }'
```

**Request Body:**
```json
{
  "cpu_usage": 45.2,      // CPU usage percentage (0-100)
  "memory_usage": 68.5,   // Memory usage percentage (0-100)
  "disk_usage": 72.1      // Disk usage percentage (0-100)
}
```

**Response:**
```json
{
  "message": "metrics updated"
}
```

**Important Notes:**
- Sending metrics automatically updates the server status to "online"
- Metrics are stored in a time-series database with 30-day retention
- Missing metrics updates may cause the server to be marked as "offline" or "degraded"

### 3. Agent Metrics Collection Example

Example Python agent implementation:

```python
import requests
import psutil
import time
import os

SERVER_ID = os.getenv("SERVER_ID")
API_URL = os.getenv("API_URL", "https://api.example.com")
TOKEN = os.getenv("AUTH_TOKEN")

def collect_metrics():
    """Collect system metrics"""
    return {
        "cpu_usage": psutil.cpu_percent(interval=1),
        "memory_usage": psutil.virtual_memory().percent,
        "disk_usage": psutil.disk_usage('/').percent
    }

def send_metrics(metrics):
    """Send metrics to API"""
    url = f"{API_URL}/servers/{SERVER_ID}/metrics"
    headers = {
        "Authorization": f"Bearer {TOKEN}",
        "Content-Type": "application/json"
    }
    
    try:
        response = requests.put(url, json=metrics, headers=headers)
        response.raise_for_status()
        print(f"✓ Metrics sent: CPU={metrics['cpu_usage']:.1f}% "
              f"MEM={metrics['memory_usage']:.1f}% "
              f"DISK={metrics['disk_usage']:.1f}%")
    except Exception as e:
        print(f"✗ Failed to send metrics: {e}")

def main():
    """Main metrics collection loop"""
    print(f"Starting metrics agent for server {SERVER_ID}")
    
    while True:
        metrics = collect_metrics()
        send_metrics(metrics)
        time.sleep(60)  # Send every 60 seconds

if __name__ == "__main__":
    main()
```

### 4. Agent Best Practices

1. **Polling Interval**: Send metrics every 30-60 seconds
2. **Error Handling**: Implement retry logic with exponential backoff
3. **Resource Usage**: Minimize agent overhead (< 1% CPU, < 50MB RAM)
4. **Graceful Shutdown**: Send final metrics before shutdown
5. **Token Management**: Refresh JWT tokens before expiry
6. **Logging**: Log metric collection failures for debugging

## For CLI Developers

### 1. List Servers with Filters

The `GET /servers` endpoint now supports filtering and includes aggregated statistics:

```bash
# Get all servers
curl -X GET "https://api.example.com/servers" \
  -H "Authorization: Bearer $TOKEN"

# Filter by status
curl -X GET "https://api.example.com/servers?status=online" \
  -H "Authorization: Bearer $TOKEN"

# Filter by location
curl -X GET "https://api.example.com/servers?location=us-east-1" \
  -H "Authorization: Bearer $TOKEN"

# Search by name or hostname
curl -X GET "https://api.example.com/servers?search=web" \
  -H "Authorization: Bearer $TOKEN"

# Combine filters with pagination
curl -X GET "https://api.example.com/servers?status=online&location=us-west-2&limit=20&offset=0" \
  -H "Authorization: Bearer $TOKEN"
```

**Query Parameters:**
- `status` (string): Filter by status (`online`, `offline`, `degraded`)
- `location` (string): Filter by location
- `search` (string): Search in server name or hostname
- `ids` (array): Filter by specific server IDs
- `tags` (map[string]string): Filter by tags as key-value pairs
- `limit` (int): Results per page (default: 50)
- `offset` (int): Pagination offset

**Response:**
```json
{
  "results": [
    {
      "id": "srv_123",
      "name": "web-server-01",
      "status": "online",
      "location": "us-east-1",
      "ip_address": "192.168.1.100",
      "hostname": "web01.example.com",
      "metrics": {
        "cpu_usage": 45.2,
        "memory_usage": 68.5,
        "disk_usage": 72.1,
        "updated_at": "2025-11-30T21:00:00Z"
      },
      "last_check_in": "2025-11-30T21:00:00Z",
      "created_at": "2025-11-01T10:00:00Z",
      "updated_at": "2025-11-30T21:00:00Z",
      "platform": { ... },
      "tags": {
        "environment": "production",
        "role": "web"
      }
    }
  ],
  "total_count": 150,
  "count": 20,
  "stats": {
    "total": 150,
    "online": 142,
    "offline": 5,
    "degraded": 3,
    "avg_cpu_usage": 42.5,
    "avg_memory_usage": 65.3
  },
  "timestamp": "2025-11-30T21:00:00Z",
  "query": { ... }
}
```

### 2. Get Server Metrics

Retrieve the latest metrics for a specific server:

```bash
curl -X GET "https://api.example.com/servers/{server_id}/metrics" \
  -H "Authorization: Bearer $TOKEN"
```

**Response:**
```json
{
  "cpu_usage": 45.2,
  "memory_usage": 68.5,
  "disk_usage": 72.1,
  "updated_at": "2025-11-30T21:00:00Z"
}
```

### 3. Get Metrics History

Retrieve historical metrics with customizable time period and resolution:

```bash
# Last 24 hours with 5-minute resolution (default)
curl -X GET "https://api.example.com/servers/{server_id}/metrics/history" \
  -H "Authorization: Bearer $TOKEN"

# Last 7 days with 1-hour resolution
curl -X GET "https://api.example.com/servers/{server_id}/metrics/history?period=7d&resolution=1h" \
  -H "Authorization: Bearer $TOKEN"

# Last hour with 1-minute resolution
curl -X GET "https://api.example.com/servers/{server_id}/metrics/history?period=1h&resolution=1m" \
  -H "Authorization: Bearer $TOKEN"
```

**Query Parameters:**
- `period` (string): Time period - `1h`, `24h`, `7d`, `30d` (default: `24h`)
- `resolution` (string): Data resolution - `1m`, `5m`, `1h` (default: `5m`)

**Response:**
```json
{
  "server_id": "srv_123",
  "period": "24h",
  "resolution": "5m",
  "data_points": [
    {
      "timestamp": "2025-11-30T20:00:00Z",
      "cpu_usage": 45.2,
      "memory_usage": 68.5,
      "disk_usage": 72.1
    },
    {
      "timestamp": "2025-11-30T20:05:00Z",
      "cpu_usage": 46.1,
      "memory_usage": 69.2,
      "disk_usage": 72.1
    }
    // ... more data points
  ]
}
```

**Period/Resolution Guidelines:**
- `1h` period: Use `1m` resolution (60 data points)
- `24h` period: Use `5m` resolution (288 data points)
- `7d` period: Use `1h` resolution (168 data points)
- `30d` period: Use `1h` resolution (720 data points)

### 4. Get Server Activity Feed

Retrieve recent activity for a server:

```bash
# Get recent activity (last 50 events)
curl -X GET "https://api.example.com/servers/{server_id}/activity" \
  -H "Authorization: Bearer $TOKEN"

# Filter by activity type
curl -X GET "https://api.example.com/servers/{server_id}/activity?type=deployment" \
  -H "Authorization: Bearer $TOKEN"

# Filter by date range
curl -X GET "https://api.example.com/servers/{server_id}/activity?from=2025-11-01T00:00:00Z&to=2025-11-30T23:59:59Z" \
  -H "Authorization: Bearer $TOKEN"

# Pagination
curl -X GET "https://api.example.com/servers/{server_id}/activity?limit=100&offset=0" \
  -H "Authorization: Bearer $TOKEN"
```

**Query Parameters:**
- `type` (string): Filter by activity type (`deployment`, `health_check`, `system_event`, `alert`, `command_execution`)
- `from` (string): Start date (RFC3339 format)
- `to` (string): End date (RFC3339 format)
- `limit` (int): Results per page (default: 50)
- `offset` (int): Pagination offset

**Response:**
```json
{
  "count": 50,
  "total_count": 256,
  "results": [
    {
      "id": "act_123",
      "server_id": "srv_123",
      "type": "command_execution",
      "description": "Executed command: systemctl restart nginx",
      "status": "success",
      "metadata": {
        "command": "systemctl restart nginx",
        "exit_code": 0,
        "duration_ms": 1234
      },
      "timestamp": "2025-11-30T20:45:00Z"
    },
    {
      "id": "act_124",
      "server_id": "srv_123",
      "type": "alert",
      "description": "Job failed: backup-script",
      "status": "failed",
      "metadata": {
        "job_id": "job_456",
        "error": "Connection timeout"
      },
      "timestamp": "2025-11-30T19:30:00Z"
    }
  ],
  "timestamp": "2025-11-30T21:00:00Z"
}
```

**Activity Types:**
- `deployment`: Software deployments and updates
- `health_check`: Health check results
- `system_event`: System-level events
- `alert`: Failed jobs and errors (last 24h)
- `command_execution`: Command executions

### 5. Get Dashboard Statistics

Retrieve aggregated statistics for the dashboard:

```bash
curl -X GET "https://api.example.com/dashboard/stats" \
  -H "Authorization: Bearer $TOKEN"
```

**Response:**
```json
{
  "servers": {
    "total": 150,
    "online": 142,
    "offline": 5,
    "degraded": 3,
    "avg_cpu_usage": 42.5,
    "avg_memory_usage": 65.3
  },
  "metrics": {
    "avg_cpu_usage": 42.5,
    "avg_memory_usage": 65.3,
    "avg_disk_usage": 0
  },
  "users": {
    "total": 0,
    "active": 0,
    "invited": 0,
    "suspended": 0
  },
  "recent_alerts": 8,
  "timestamp": "2025-11-30T21:00:00Z"
}
```

**Note:** User stats are placeholders and will be provided by a separate Users API.

### 6. Update Server Metadata

Update server location, IP, or hostname:

```bash
curl -X PATCH "https://api.example.com/servers/{server_id}" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "us-west-2",
    "ip_address": "192.168.1.101",
    "hostname": "web01-new.example.com"
  }'
```

### 7. CLI Example Commands

Example CLI implementation snippets:

```bash
# servers list --status online --location us-east-1
servers list --status online --location us-east-1 --format table

# servers metrics <server_id>
servers metrics srv_123

# servers metrics:history <server_id> --period 7d --resolution 1h
servers metrics:history srv_123 --period 7d --resolution 1h --format json

# servers activity <server_id> --type alert --limit 20
servers activity srv_123 --type alert --limit 20

# dashboard stats
dashboard stats --format json
```

## Server Status States

Servers can be in one of three states:

| Status | Description | Triggers |
|--------|-------------|----------|
| `online` | Server is healthy and reporting | Metrics received within expected interval |
| `offline` | Server is not responding | No metrics for extended period |
| `degraded` | Server is experiencing issues | High resource usage or partial failures |

**Status Determination:**
- Sending metrics automatically sets status to `online`
- Status transitions should be monitored via the activity feed

## Authentication & Authorization

All endpoints require JWT authentication with appropriate scopes:

| Endpoint | Required Scope |
|----------|---------------|
| `GET /servers` | `read:servers` |
| `PUT /servers/:id/metrics` | `write:servers` |
| `GET /servers/:id/metrics` | `read:servers` |
| `GET /servers/:id/metrics/history` | `read:servers` |
| `GET /servers/:id/activity` | `read:servers` |
| `GET /dashboard/stats` | `read:dashboard` |

## Data Retention

- **Metrics History**: 30 days maximum, 7 days default
- **Activity Feed**: Based on job retention policy
- **Server Records**: Indefinite until explicitly deleted

## Rate Limits

Recommended limits for agent implementations:

- **Metrics Updates**: 1 request per 30-60 seconds per server
- **Metrics History**: 10 requests per minute
- **Activity Feed**: 20 requests per minute
- **Dashboard Stats**: 5 requests per minute

## Error Handling

Common error responses:

```json
// 400 Bad Request
{
  "error": "invalid period value, must be one of: 1h, 24h, 7d, 30d"
}

// 401 Unauthorized
{
  "error": "invalid or expired token"
}

// 403 Forbidden
{
  "error": "insufficient permissions"
}

// 404 Not Found
{
  "error": "server not found"
}

// 500 Internal Server Error
{
  "error": "internal server error"
}
```

## Migration Guide

### Existing Agents

1. **Add Metrics Collection**: Implement metrics collection using `psutil` or equivalent
2. **Add Periodic Sender**: Create a background thread to send metrics every 60 seconds
3. **Update Registration**: Include location, IP address, and hostname during registration
4. **Test**: Verify metrics appear in the API and dashboard

### Existing CLIs

1. **Update List Command**: Add support for new query parameters (status, location, search)
2. **Add Metrics Commands**: Implement `metrics` and `metrics:history` commands
3. **Add Activity Command**: Implement `activity` command with filtering
4. **Add Dashboard Command**: Implement `dashboard stats` command
5. **Update Output**: Display new server fields (status, location, metrics, last_check_in)

## Testing

### Test Agent Metrics

```bash
# Send test metrics
curl -X PUT https://api.example.com/servers/srv_123/metrics \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"cpu_usage": 50, "memory_usage": 60, "disk_usage": 70}'

# Verify metrics were stored
curl -X GET https://api.example.com/servers/srv_123/metrics \
  -H "Authorization: Bearer $TOKEN"

# Check metrics history after a few updates
curl -X GET "https://api.example.com/servers/srv_123/metrics/history?period=1h&resolution=1m" \
  -H "Authorization: Bearer $TOKEN"
```

### Test Filtering

```bash
# Create servers with different statuses and locations (via metrics updates)
# Then test filtering:
curl -X GET "https://api.example.com/servers?status=online" \
  -H "Authorization: Bearer $TOKEN"

curl -X GET "https://api.example.com/servers?location=us-east-1" \
  -H "Authorization: Bearer $TOKEN"

curl -X GET "https://api.example.com/servers?search=web" \
  -H "Authorization: Bearer $TOKEN"
```

## Support & Resources

- **API Documentation**: https://api.example.com/docs
- **OpenAPI Spec**: https://api.example.com/swagger.json
- **GitHub Repository**: https://github.com/ambientlabscomputing/server_api

## Breaking Changes

None. All changes are backward compatible:
- New server fields are optional
- Existing endpoints maintain their original behavior
- New endpoints use different paths

## Changelog

**Version 2.0.0** (November 2025)
- Added server status field (online/offline/degraded)
- Added location, IP address, and hostname fields
- Added metrics tracking and history
- Added server activity feed
- Added dashboard statistics
- Enhanced server query with filtering
- Added time-series metrics storage with 30-day retention
