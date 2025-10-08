#!/bin/bash
# Helm Package and Index Creation Script
# Usage: ./package-and-index.sh

set -e

# Configuration
CHART_DIR=".."
PACKAGES_DIR="."
REPO_URL="https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/"
CHART_NAME="aws-node-termination-handler"

echo "🚀 Starting Helm package and index creation..."

# Step 1: Validate chart
echo "📋 Validating chart..."
helm lint $CHART_DIR

# Step 2: Test template generation
echo "🧪 Testing template generation..."
helm template $CHART_NAME $CHART_DIR --dry-run > /dev/null

# Step 3: Create package
echo "📦 Creating Helm package..."
helm package $CHART_DIR

# Step 4: Create packages directory if it doesn't exist
mkdir -p $PACKAGES_DIR

# Step 5: Move package to packages directory
# echo "📁 Moving package to packages directory..."
# mv *.tgz $PACKAGES_DIR/

# Step 6: Generate index
echo "📋 Generating index.yaml..."
helm repo index $PACKAGES_DIR/ --url $REPO_URL

# Step 7: Verify results
echo "✅ Package and index created successfully!"
echo "📁 Files created:"
ls -la $PACKAGES_DIR/

echo ""
echo "🎯 Next steps:"
echo "1. Review the generated files"
echo "2. Commit and push to GitHub:"
echo "   git add ."
echo "   git commit -m 'Release Helm chart v1.0.0'"
echo "   git push origin main"
echo ""
echo "3. Test your repository:"
echo "   helm repo add banana-nth $REPO_URL"
echo "   helm repo update"
echo "   helm search repo banana-nth"
echo ""
echo "🎉 Done!"
