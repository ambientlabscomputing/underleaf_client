#!/bin/bash

# Start test cluster for mDNS testing
# This script starts 3 nodes: node1 (bootstrap), node2, node3

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
AGENT_BIN="$SCRIPT_DIR/../underleaf_agent"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if agent binary exists
if [ ! -f "$AGENT_BIN" ]; then
    echo -e "${RED}Error: underleaf_agent binary not found. Run 'make build' first.${NC}"
    exit 1
fi

# Clean up any previous test data
echo -e "${BLUE}Cleaning up previous test data...${NC}"
rm -rf /tmp/underleaf_test
mkdir -p /tmp/underleaf_test/{node1,node2,node3}/{raft,logs}

# Kill any existing test agents
pkill -f "underleaf_agent.*test_cluster" || true
sleep 1

echo -e "${GREEN}Starting test cluster...${NC}"
echo ""

# Start node1 (bootstrap node)
echo -e "${BLUE}Starting node1 (bootstrap)...${NC}"
# Copy config to node's temp directory
cp "$SCRIPT_DIR/config_node1.yaml" /tmp/underleaf_test/node1/config.yaml
cd /tmp/underleaf_test/node1 && \
TMPDIR="/tmp/underleaf_test/node1/" \
    "$AGENT_BIN" serve --port 8081 --mode dev > logs/agent.log 2>&1 &
NODE1_PID=$!
cd "$SCRIPT_DIR"
echo "Node1 PID: $NODE1_PID (port 8081, raft 7001)"

# Wait for node1 to start
sleep 3

# Start node2
echo -e "${BLUE}Starting node2...${NC}"
cp "$SCRIPT_DIR/config_node2.yaml" /tmp/underleaf_test/node2/config.yaml
cd /tmp/underleaf_test/node2 && \
TMPDIR="/tmp/underleaf_test/node2/" \
    "$AGENT_BIN" serve --port 8082 --mode dev > logs/agent.log 2>&1 &
NODE2_PID=$!
cd "$SCRIPT_DIR"
echo "Node2 PID: $NODE2_PID (port 8082, raft 7002)"

# Wait for node2 to join
sleep 2

# Start node3
echo -e "${BLUE}Starting node3...${NC}"
cp "$SCRIPT_DIR/config_node3.yaml" /tmp/underleaf_test/node3/config.yaml
cd /tmp/underleaf_test/node3 && \
TMPDIR="/tmp/underleaf_test/node3/" \
    "$AGENT_BIN" serve --port 8083 --mode dev > logs/agent.log 2>&1 &
NODE3_PID=$!
cd "$SCRIPT_DIR"
echo "Node3 PID: $NODE3_PID (port 8083, raft 7003)"

echo ""
echo -e "${GREEN}Test cluster started!${NC}"
echo ""
echo "Cluster information:"
echo "  Node1: http://localhost:8081 (Raft: 127.0.0.1:7001)"
echo "  Node2: http://localhost:8082 (Raft: 127.0.0.1:7002)"
echo "  Node3: http://localhost:8083 (Raft: 127.0.0.1:7003)"
echo ""
echo "Logs:"
echo "  Node1: /tmp/underleaf_test/node1/logs/agent.log"
echo "  Node2: /tmp/underleaf_test/node2/logs/agent.log"
echo "  Node3: /tmp/underleaf_test/node3/logs/agent.log"
echo ""
echo "To discover mDNS services, run:"
echo "  dns-sd -B _underleaf._tcp local"
echo ""
echo "To resolve a specific service:"
echo "  dns-sd -L api.underleaf _underleaf._tcp local"
echo "  dns-sd -L node1.underleaf _underleaf._tcp local"
echo ""
echo "To tail logs:"
echo "  tail -f /tmp/underleaf_test/node1/logs/agent.log"
echo ""
echo "To stop the cluster:"
echo "  bash $SCRIPT_DIR/cleanup.sh"
