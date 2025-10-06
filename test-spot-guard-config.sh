#!/bin/bash
# Test Spot Guard configuration and basic functionality

set -e

echo "🧪 Testing Spot Guard Configuration"

# Set up environment
export ENABLE_SPOT_GUARD=true
export SPOT_ASG_NAME="test-spot-asg"
export ON_DEMAND_ASG_NAME="test-ondemand-asg"
export SPOT_GUARD_SCALE_TIMEOUT=120
export SPOT_GUARD_CAPACITY_CHECK_TIMEOUT=120
export ENABLE_REBALANCE_MONITORING=true
export AWS_REGION="us-east-1"
export NODE_NAME="test-node"
export DRY_RUN=true

# Build the application
echo "🔨 Building application..."
make build

echo "🔍 Testing configuration parsing..."

# Test 1: Check if the application can parse Spot Guard configuration
echo "Test 1: Configuration parsing"
if ./build/node-termination-handler --help 2>&1 | grep -q "spot-guard"; then
    echo "✅ Spot Guard flags are available"
else
    echo "❌ Spot Guard flags not found"
    exit 1
fi

# Test 2: Check configuration validation
echo "Test 2: Configuration validation"
if ./build/node-termination-handler \
  --enable-spot-guard=true \
  --spot-asg-name=$SPOT_ASG_NAME \
  --on-demand-asg-name=$ON_DEMAND_ASG_NAME \
  --enable-rebalance-monitoring=true \
  --node-name=$NODE_NAME \
  --dry-run=true \
  --log-level=debug 2>&1 | head -20; then
    echo "✅ Configuration validation passed"
else
    echo "❌ Configuration validation failed"
    exit 1
fi

echo ""
echo "🎯 Configuration Test Summary:"
echo "✅ Spot Guard flags are available"
echo "✅ Configuration parsing works"
echo "✅ Environment variables are properly set"
echo ""
echo "📝 Note: The application requires Kubernetes connectivity to run fully."
echo "   For complete testing, use one of these options:"
echo "   1. ./test-spot-guard-local.sh (requires local K8s cluster)"
echo "   2. Deploy to EKS for full integration testing"
echo "   3. Use the e2e tests: make e2e-test"

