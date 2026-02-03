#!/bin/bash
set -e

# E2E Test Script for Underleaf Agent
# Tests Raft clustering with multiple local processes
#
# This script:
# 1. Creates 3 agent configs in separate temp directories
# 2. Runs ufctl auth login for each agent (authenticates with control plane)
#    - Uses XDG_STATE_HOME to save tokens to each agent's config
#    - Opens browser 3 times for OAuth flows
# 3. Starts 3 agent processes on ports 11001-11003 (HTTP) and 11101-11103 (Raft)
# 4. Forms a Raft cluster by joining nodes 2 and 3 to node 1
# 5. Runs tests: health checks, leader election, KV operations, leader failover
# 6. Uses --config-agent-port flag to specify which agent to connect to

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEST_DIR="/tmp/underleaf-e2e-$$"
AGENT_BIN="$PROJECT_ROOT/underleaf_agent"
UFCTL_BIN="$PROJECT_ROOT/ufctl"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test state
PIDS=()
FAILED=0

# Cleanup function
cleanup() {
    echo ""
    echo "Cleaning up..."
    
    # Kill all agent processes
    for pid in "${PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            echo "Killing process $pid"
            kill "$pid" 2>/dev/null || true
        fi
    done
    
    # Wait a bit for graceful shutdown
    sleep 1
    
    # Force kill if still running
    for pid in "${PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill -9 "$pid" 2>/dev/null || true
        fi
    done
    
    # Remove any stale PID files
    rm -f /tmp/underleaf-agent.pid 2>/dev/null || true
    
    # Clean up test directory only on success
    if [ $FAILED -eq 0 ]; then
        if [ -d "$TEST_DIR" ]; then
            rm -rf "$TEST_DIR"
        fi
        echo -e "${GREEN}✓ All tests passed${NC}"
        exit 0
    else
        echo -e "${YELLOW}Test directory preserved for debugging: $TEST_DIR${NC}"
        echo -e "${RED}✗ $FAILED test(s) failed${NC}"
        exit 1
    fi
}

trap cleanup EXIT INT TERM

# Helper functions
log() {
    echo -e "${GREEN}[TEST]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    FAILED=$((FAILED + 1))
}

success() {
    echo -e "${GREEN}✓${NC} $1"
}

wait_for_health() {
    local port=$1
    local timeout=30
    local elapsed=0
    
    while [ $elapsed -lt $timeout ]; do
        if curl -s -f "http://localhost:$port/health" > /dev/null 2>&1; then
            return 0
        fi
        sleep 0.5
        elapsed=$((elapsed + 1))
    done
    
    return 1
}

assert_equal() {
    local expected=$1
    local actual=$2
    local message=$3
    
    if [ "$expected" = "$actual" ]; then
        success "$message"
    else
        error "$message (expected: $expected, got: $actual)"
    fi
}

assert_contains() {
    local haystack=$1
    local needle=$2
    local message=$3
    
    if echo "$haystack" | grep -q "$needle"; then
        success "$message"
    else
        error "$message (expected to contain: $needle)"
    fi
}

# Check binaries exist
if [ ! -f "$AGENT_BIN" ]; then
    error "Agent binary not found: $AGENT_BIN"
    error "Run 'make build' first"
    exit 1
fi

if [ ! -f "$UFCTL_BIN" ]; then
    error "ufctl binary not found: $UFCTL_BIN"
    error "Run 'make build' first"
    exit 1
fi

log "Starting E2E tests..."
log "Test directory: $TEST_DIR"

# Create test directory structure
mkdir -p "$TEST_DIR"/{node1,node2,node3}/{config,data,state/underleaf}

# Try to copy tokens from a previous test run to avoid re-authentication
PREV_TEST_DIR=$(ls -dt /tmp/underleaf-e2e-* 2>/dev/null | grep -v "$$" | head -1)
if [ -n "$PREV_TEST_DIR" ] && [ -d "$PREV_TEST_DIR" ]; then
    log "Found previous test directory: $PREV_TEST_DIR"
    for i in 1 2 3; do
        if [ -f "$PREV_TEST_DIR/node$i/.underleaf/config.yaml" ]; then
            mkdir -p "$TEST_DIR/node$i/.underleaf"
            cp "$PREV_TEST_DIR/node$i/.underleaf/config.yaml" "$TEST_DIR/node$i/.underleaf/" 2>/dev/null || true
        fi
    done
fi

# Authenticate each node (this will open browser for each one)
# Check if already authenticated to skip browser flow
log "Authenticating nodes..."

for i in 1 2 3; do
    node_config="$TEST_DIR/node$i/.underleaf/config.yaml"
    if [ -f "$node_config" ] && grep -q "token:" "$node_config" 2>/dev/null; then
        log "Node$i already authenticated, skipping..."
    else
        echo ""
        echo "========================================="
        echo "AUTHENTICATING NODE $i"
        echo "========================================="
        HOME="$TEST_DIR/node$i" "$UFCTL_BIN" auth login
    fi
done

echo ""
log "Authentication complete, updating configs with Raft settings..."

# Now update the configs to add Raft settings and server IDs
# Use Python to properly merge YAML to avoid duplicate keys
for i in 1 2 3; do
    python3 << EOF
import yaml

config_file = "$TEST_DIR/node$i/.underleaf/config.yaml"
with open(config_file, 'r') as f:
    config = yaml.safe_load(f) or {}

# Add server_id to local section
if 'local' not in config:
    config['local'] = {}
config['local']['server_id'] = 'test-server-node$i'

# Add Raft configuration
config['raft'] = {
    'enabled': True,
    'node_id': 'node$i',
    'bind_addr': '127.0.0.1:1110$i',
    'data_dir': '$TEST_DIR/node$i/data/raft',
    'bootstrap': $i == 1
}

# Add mDNS configuration
config['mdns'] = {
    'enabled': False
}

with open(config_file, 'w') as f:
    yaml.dump(config, f, default_flow_style=False)
EOF
done

# Clean up any old Raft data from previous failed test runs
log "Cleaning up old Raft data..."
for i in 1 2 3; do
    rm -rf "$TEST_DIR/node$i/data/raft" 2>/dev/null || true
done

# Clean up any stale PID files
rm -f /tmp/underleaf-agent.pid 2>/dev/null || true

# Start agent processes with authenticated configs
log "Starting node1 (bootstrap) on port 11001..."
HOME="$TEST_DIR/node1" \
    "$AGENT_BIN" serve --port 11001 --mode dev \
    > "$TEST_DIR/node1/agent.log" 2>&1 &
PIDS+=($!)

log "Starting node2 on port 11002..."
HOME="$TEST_DIR/node2" \
    "$AGENT_BIN" serve --port 11002 --mode dev \
    > "$TEST_DIR/node2/agent.log" 2>&1 &
PIDS+=($!)

log "Starting node3 on port 11003..."
HOME="$TEST_DIR/node3" \
    "$AGENT_BIN" serve --port 11003 --mode dev \
    > "$TEST_DIR/node3/agent.log" 2>&1 &
PIDS+=($!)

log "Waiting for agents to initialize..."
sleep 8

# Wait for health checks
log "Checking health endpoints..."
if ! wait_for_health 11001; then
    error "Node1 failed to start (see $TEST_DIR/node1/agent.log)"
    cat "$TEST_DIR/node1/agent.log"
    exit 1
fi
success "Node1 healthy"

if ! wait_for_health 11002; then
    error "Node2 failed to start (see $TEST_DIR/node2/agent.log)"
    cat "$TEST_DIR/node2/agent.log"
    exit 1
fi
success "Node2 healthy"

if ! wait_for_health 11003; then
    error "Node3 failed to start (see $TEST_DIR/node3/agent.log)"
    cat "$TEST_DIR/node3/agent.log"
    exit 1
fi
success "Node3 healthy"

# Join nodes to cluster
log "Forming Raft cluster..."
if "$UFCTL_BIN" cluster join node2 127.0.0.1:11102 --config-agent-port 11001 2>/dev/null; then
    success "Node2 joined cluster"
else
    error "Failed to join node2 to cluster"
fi

log "Waiting for cluster to stabilize after node2 join..."
sleep 3

if "$UFCTL_BIN" cluster join node3 127.0.0.1:11103 --config-agent-port 11001 2>/dev/null; then
    success "Node3 joined cluster"
else
    error "Failed to join node3 to cluster"
fi

# Nodes are automatically added as voters when using join command
# No need for manual promotion

# Wait for cluster to stabilize and elect leader
log "Waiting for Raft cluster to stabilize..."
sleep 8

# Test 1: Health checks
log "Test 1: Health checks"
response1=$(curl -s http://localhost:11001/health)
assert_contains "$response1" '"status":"healthy"' "Node1 health check"

response2=$(curl -s http://localhost:11002/health)
assert_contains "$response2" '"status":"healthy"' "Node2 health check"

response3=$(curl -s http://localhost:11003/health)
assert_contains "$response3" '"status":"healthy"' "Node3 health check"

# Test 2: Cluster status - wait for leader election
log "Test 2: Cluster status and leader election"
sleep 5  # Give Raft time to elect a leader

status=$(curl -s http://localhost:11001/api/v1/cluster/status || echo '{}')
assert_contains "$status" '"node_id"' "Cluster status contains node_id"

# Check if we have a leader
leader_count=0
for port in 11001 11002 11003; do
    status=$(curl -s http://localhost:$port/api/v1/cluster/status || echo '{}')
    if echo "$status" | grep -q '"role":"leader"'; then
        leader_count=$((leader_count + 1))
        success "Found leader on port $port"
    fi
done

if [ $leader_count -eq 1 ]; then
    success "Exactly one leader elected"
elif [ $leader_count -eq 0 ]; then
    error "No leader elected"
else
    error "Multiple leaders elected ($leader_count)"
fi

# Test 3: KV operations (if cluster is healthy)
log "Test 3: KV store operations"

# Try to find the leader port
LEADER_PORT=""
for port in 11001 11002 11003; do
    status=$(curl -s http://localhost:$port/api/v1/cluster/status || echo '{}')
    if echo "$status" | grep -q '"role":"leader"'; then
        LEADER_PORT=$port
        break
    fi
done

if [ -n "$LEADER_PORT" ]; then
    log "Testing KV operations on leader (port $LEADER_PORT)"
    
    # Set a value
    if "$UFCTL_BIN" cluster kv put test-key test-value --config-agent-port "$LEADER_PORT" 2>/dev/null; then
        success "KV set operation"
    else
        error "KV set operation failed"
    fi
    
    # Get the value
    value=$("$UFCTL_BIN" cluster kv get test-key --config-agent-port "$LEADER_PORT" 2>/dev/null || echo "")
    assert_contains "$value" "test-value" "KV get operation"
    
    # Follower reads require ?mode=stale query parameter (not yet supported in CLI)
    # Skipping follower read test for now
else
    log "Skipping KV tests - no leader found"
fi

# Test 4: Leader failure and re-election
log "Test 4: Leader failure and re-election"

if [ -n "$LEADER_PORT" ]; then
    # Find the leader PID
    LEADER_PID=""
    case $LEADER_PORT in
        11001) LEADER_PID=${PIDS[0]} ;;
        11002) LEADER_PID=${PIDS[1]} ;;
        11003) LEADER_PID=${PIDS[2]} ;;
    esac
    
    if [ -n "$LEADER_PID" ]; then
        log "Killing leader on port $LEADER_PORT (PID $LEADER_PID)"
        kill "$LEADER_PID" 2>/dev/null || true
        sleep 5  # Wait for re-election (increased from 3s)
        
        # Check for new leader
        new_leader_count=0
        for port in 11001 11002 11003; do
            if [ "$port" = "$LEADER_PORT" ]; then
                continue  # Skip the old leader
            fi
            status=$(curl -s http://localhost:$port/api/v1/cluster/status 2>/dev/null || echo '{}')
            if echo "$status" | grep -q '"role":"leader"'; then
                new_leader_count=$((new_leader_count + 1))
                success "New leader elected on port $port"
            fi
        done
        
        if [ $new_leader_count -eq 1 ]; then
            success "Leader re-election successful"
        else
            error "Leader re-election failed (found $new_leader_count leaders)"
        fi
    fi
else
    log "Skipping leader failure test - no initial leader found"
fi

log "E2E tests complete"
