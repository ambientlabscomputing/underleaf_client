#!/bin/bash

# Cleanup script for test cluster
echo "Cleaning up test cluster..."

# Kill any running test agents
pkill -f "underleaf_agent.*test_cluster" || true

# Remove test data directories
rm -rf /tmp/underleaf_test

echo "Cleanup complete!"
