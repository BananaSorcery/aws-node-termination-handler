#!/bin/bash
# Test Spot Guard in dry run mode

set -e

echo "🧪 Testing Spot Guard in Dry Run Mode"

# Set up environment
export ENABLE_SPOT_GUARD=true
export SPOT_ASG_NAME="your-spot-asg-name"
export ON_DEMAND_ASG_NAME="your-ondemand-asg-name"
export SPOT_GUARD_SCALE_TIMEOUT=120
export SPOT_GUARD_CAPACITY_CHECK_TIMEOUT=120
export ENABLE_REBALANCE_MONITORING=true
export AWS_REGION="us-east-1"
export NODE_NAME=$(hostname)
export DRY_RUN=true

# Build the application
echo "🔨 Building application..."
make build

# Run with dry run enabled
echo "🚀 Running NTH with Spot Guard in dry run mode..."
echo "This will log what actions would be taken without executing them"
echo ""

./build/node-termination-handler \
  --enable-spot-guard=true \
  --spot-asg-name=$SPOT_ASG_NAME \
  --on-demand-asg-name=$ON_DEMAND_ASG_NAME \
  --enable-rebalance-monitoring=true \
  --node-name=$NODE_NAME \
  --dry-run=true \
  --log-level=debug
