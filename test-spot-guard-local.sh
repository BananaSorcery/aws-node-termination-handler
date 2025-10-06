#!/bin/bash
# Test Spot Guard locally with minimal Kubernetes setup

set -e

echo "🧪 Testing Spot Guard with Local Kubernetes Setup"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl not found. Please install kubectl first."
    echo "   You can install it with: curl -LO https://dl.k8s.io/release/\$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
    exit 1
fi

# Check if we have a kubeconfig
if [ -z "$KUBECONFIG" ] && [ ! -f "$HOME/.kube/config" ]; then
    echo "❌ No Kubernetes configuration found."
    echo "   Please set up a local Kubernetes cluster or set KUBECONFIG"
    echo ""
    echo "   Options:"
    echo "   1. Use kind: kind create cluster --name test-cluster"
    echo "   2. Use minikube: minikube start"
    echo "   3. Use k3s: curl -sfL https://get.k3s.io | sh -"
    exit 1
fi

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

# Create a test node in Kubernetes (if it doesn't exist)
echo "🏗️  Setting up test node in Kubernetes..."
kubectl get nodes test-node 2>/dev/null || kubectl create -f - <<EOF
apiVersion: v1
kind: Node
metadata:
  name: test-node
  labels:
    kubernetes.io/hostname: test-node
    node-role.kubernetes.io/worker: ""
spec:
  unschedulable: false
status:
  conditions:
  - type: Ready
    status: "True"
  - type: MemoryPressure
    status: "False"
  - type: DiskPressure
    status: "False"
  - type: PIDPressure
    status: "False"
  nodeInfo:
    architecture: amd64
    operatingSystem: linux
    kernelVersion: 5.4.0
    kubeletVersion: v1.20.0
    kubeProxyVersion: v1.20.0
    containerRuntimeVersion: containerd://1.4.0
EOF

echo "🚀 Running NTH with Spot Guard in dry run mode..."
echo "This will log what actions would be taken without executing them"
echo ""

# Run the application
./build/node-termination-handler \
  --enable-spot-guard=true \
  --spot-asg-name=$SPOT_ASG_NAME \
  --on-demand-asg-name=$ON_DEMAND_ASG_NAME \
  --enable-rebalance-monitoring=true \
  --node-name=$NODE_NAME \
  --dry-run=true \
  --log-level=debug &

# Store the PID
NTH_PID=$!

# Wait a bit for the application to start
sleep 5

# Check if the process is still running
if ! kill -0 $NTH_PID 2>/dev/null; then
    echo "❌ Application failed to start or crashed"
    exit 1
fi

echo "✅ Application started successfully (PID: $NTH_PID)"
echo "📝 Check the logs above for Spot Guard configuration and behavior"
echo ""
echo "To stop the test, run: kill $NTH_PID"
echo "Or press Ctrl+C"

# Wait for user to stop
trap "echo '🛑 Stopping test...'; kill $NTH_PID 2>/dev/null; exit 0" INT
wait $NTH_PID

