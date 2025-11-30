# Command Whitelist & Blacklist Implementation Guide

## Overview
The API now supports whitelist and blacklist management for command execution on servers. These lists are stored in the server's configuration and returned when clients request server details.

## Server Configuration Structure
```json
{
  "config": {
    "payload": {
      "commands": {
        "allow_literal_commands": false,
        "whitelist": ["npm", "git", "docker"],
        "blacklist": ["rm -rf", "dd if="]
      }
    }
  }
}
```

## API Endpoints

### 1. Update Server Settings (Batch)
**PATCH** `/api/v1/servers/{id}`

Update all settings at once, including whitelist and blacklist arrays.

```json
{
  "settings": {
    "allow_literal_commands": true,
    "whitelist": ["npm", "git", "docker", "node"],
    "blacklist": ["rm -rf", "dd if=", "mkfs"]
  }
}
```

### 2. Add to Whitelist
**POST** `/api/v1/servers/{id}/whitelist`

Add a single pattern to the whitelist.

```json
{
  "pattern": "npm"
}
```

**Response:**
```json
{
  "message": "Pattern added to whitelist",
  "whitelist": ["npm", "git"],
  "blacklist": ["rm -rf"],
  "timestamp": "2025-11-27T11:00:00Z"
}
```

### 3. Remove from Whitelist
**DELETE** `/api/v1/servers/{id}/whitelist`

Remove a pattern from the whitelist.

```json
{
  "pattern": "npm"
}
```

### 4. Add to Blacklist
**POST** `/api/v1/servers/{id}/blacklist`

Add a single pattern to the blacklist.

```json
{
  "pattern": "rm -rf"
}
```

**Response:**
```json
{
  "message": "Pattern added to blacklist",
  "whitelist": ["npm", "git"],
  "blacklist": ["rm -rf", "dd if="],
  "timestamp": "2025-11-27T11:00:00Z"
}
```

### 5. Remove from Blacklist
**DELETE** `/api/v1/servers/{id}/blacklist`

Remove a pattern from the blacklist.

```json
{
  "pattern": "rm -rf"
}
```

### 6. Get Server Details
**GET** `/api/v1/servers/{id}`

Retrieve complete server configuration including whitelist and blacklist.

## Client Implementation

When a server client requests its configuration, the whitelist and blacklist are included in the response. The client should:

1. **Fetch Configuration** on startup or periodically:
```bash
GET /api/v1/servers/{server_id}
```

2. **Extract Command Settings**:
```go
whitelist := server.Config.Payload.Commands.Whitelist
blacklist := server.Config.Payload.Commands.Blacklist
allowLiteralCommands := server.Config.Payload.Commands.AllowLiteralCommands
```

3. **Validate Commands Before Execution**:
```go
func isCommandAllowed(command string, whitelist, blacklist []string) bool {
    // Check blacklist first (highest priority)
    for _, pattern := range blacklist {
        if strings.Contains(command, pattern) {
            return false
        }
    }
    
    // If whitelist is empty, allow all non-blacklisted commands
    if len(whitelist) == 0 {
        return true
    }
    
    // Check whitelist
    for _, pattern := range whitelist {
        if strings.HasPrefix(command, pattern) {
            return true
        }
    }
    
    return false
}
```

## Pattern Matching Guidelines

### Whitelist Patterns
- Should be command prefixes (e.g., `"npm"`, `"git"`, `"docker"`)
- Commands matching these prefixes are allowed
- Empty whitelist = all commands allowed (except blacklisted)

### Blacklist Patterns
- Can be full commands or dangerous patterns (e.g., `"rm -rf"`, `"dd if="`)
- Blacklist takes precedence over whitelist
- Use for preventing destructive operations

## Example Workflow

### Initial Setup
```bash
# Create server
POST /api/v1/servers
{
  "name": "prod-server-01",
  "platform": {"os": "linux", "arch": "amd64"}
}

# Add whitelist patterns
POST /api/v1/servers/{id}/whitelist
{"pattern": "npm"}

POST /api/v1/servers/{id}/whitelist
{"pattern": "git"}

# Add blacklist patterns
POST /api/v1/servers/{id}/blacklist
{"pattern": "rm -rf"}
```

### Client Validation
```bash
# Client fetches config
GET /api/v1/servers/{id}

# Returns:
{
  "config": {
    "payload": {
      "commands": {
        "whitelist": ["npm", "git"],
        "blacklist": ["rm -rf"]
      }
    }
  }
}

# Client validates:
✅ "npm install express" → Allowed (whitelist)
✅ "git clone repo" → Allowed (whitelist)
❌ "rm -rf /" → Blocked (blacklist)
❌ "python script.py" → Blocked (not in whitelist)
```

## Best Practices

1. **Start Restrictive**: Begin with a whitelist of only necessary commands
2. **Blacklist Dangerous Commands**: Always block destructive operations
3. **Regular Updates**: Review and update lists as requirements change
4. **Pattern Specificity**: Use specific patterns to avoid overly broad restrictions
5. **Test Thoroughly**: Validate patterns don't block legitimate operations

## Security Notes

- Blacklist patterns are matched using `strings.Contains()`
- Whitelist patterns should be matched using `strings.HasPrefix()`
- Blacklist takes precedence over whitelist
- Empty whitelist means "allow all" (except blacklisted)
- Server clients are responsible for enforcing these rules
- The API only stores and returns the configuration
