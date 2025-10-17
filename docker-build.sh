#!/bin/bash
# Force update script - uses new tag to ensure fresh pull

set -e

echo "🔄 Force Update: Building with new tag to bypass cache"
echo "======================================================="

# Use timestamp for unique tag
NEW_TAG="compatible-$(date +%Y%m%d-%H%M%S)"

echo ""
echo "📦 New Image Tag: ${NEW_TAG}"
echo ""

# Build
export IMG=043701111869.dkr.ecr.us-west-2.amazonaws.com/aws-node-termination-handler
export IMG_TAG=${NEW_TAG}

echo "🔨 Building image..."
make docker-build-v1

# Tag
echo "🏷️  Tagging image..."
docker tag ${IMG}:${IMG_TAG}-v1 ${IMG}:${IMG_TAG}

# Login and push
echo "📤 Pushing to ECR..."
aws ecr get-login-password --region us-west-2 | \
  docker login --username AWS --password-stdin 043701111869.dkr.ecr.us-west-2.amazonaws.com

docker push ${IMG}:${IMG_TAG}

echo ""
echo "✅ Image pushed successfully!"
echo ""
echo "📋 Deploy Command:"
echo "   kubectl delete pods -n kube-system -l app=aws-node-termination-handler"
echo ""
echo "   helm upgrade aws-nth ./config/helm/aws-node-termination-handler \\"
echo "     --namespace kube-system \\"
echo "     --set image.tag=${NEW_TAG} \\"
echo "     --set image.pullPolicy=Always \\"
echo "     --reuse-values"
echo ""
echo "🔍 Verify Command:"
echo "   kubectl get pods -n kube-system -l app=aws-node-termination-handler -o wide"
echo ""
echo "📝 Check Logs:"
echo "   kubectl logs -n kube-system -l app=aws-node-termination-handler --tail=50 | grep 'Detect'"
echo ""
