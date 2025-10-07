# Helm Packaging and Indexing Guide

This guide explains how to create, package, and index Helm charts for distribution via GitHub.

## 📦 Helm Package Creation

### Prerequisites
- Helm CLI installed (`helm version`)
- Git repository with Helm chart
- GitHub repository for hosting

### Step 1: Prepare Your Chart
```bash
# Navigate to your chart directory
cd config/helm/aws-node-termination-handler

# Validate chart structure
helm lint .

# Test template generation
helm template aws-node-termination-handler . --dry-run
```

### Step 2: Update Chart Version
```bash
# Edit Chart.yaml to update version
version: 1.0.0
appVersion: 1.0.0
```

### Step 3: Create Helm Package
```bash
# Create the package
helm package .

# This creates: aws-node-termination-handler-1.0.0.tgz
```

### Step 4: Verify Package
```bash
# List package contents
tar -tzf aws-node-termination-handler-1.0.0.tgz

# Test install from package
helm install test-release ./aws-node-termination-handler-1.0.0.tgz --dry-run
```

## 📋 Helm Index Creation

### Step 1: Create Packages Directory
```bash
# Create packages directory
mkdir -p packages/

# Move package to packages directory
mv aws-node-termination-handler-1.0.0.tgz packages/
```

### Step 2: Generate Index File
```bash
# Generate index.yaml for GitHub hosting
helm repo index packages/ --url https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/
```

### Step 3: Verify Index File
```bash
# Check generated index.yaml
cat packages/index.yaml

# Test repository
helm repo add test-repo ./packages/
helm repo update
helm search repo test-repo
```

## 🚀 GitHub Repository Setup

### Step 1: Repository Structure
```
config/helm/aws-node-termination-handler/packages/
├── aws-node-termination-handler-1.0.0.tgz
├── index.yaml
├── README.md
└── HELM_PACKAGING_GUIDE.md
```

### Step 2: Push to GitHub
```bash
# Add all files
git add .

# Commit changes
git commit -m "Add Helm chart v1.0.0 with Spot Guard feature"

# Push to GitHub
git push origin main
```

### Step 3: Verify GitHub Hosting
```bash
# Test repository URL
curl -s https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/index.yaml

# Add repository
helm repo add banana-nth https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/

# Update repository
helm repo update

# Search charts
helm search repo banana-nth
```

## 🔄 Version Management

### Creating New Versions
```bash
# 1. Update Chart.yaml version
version: 1.0.1

# 2. Create new package
helm package .

# 3. Move to packages directory
mv aws-node-termination-handler-1.0.1.tgz packages/

# 4. Regenerate index (includes all versions)
helm repo index packages/ --url https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/

# 5. Commit and push
git add .
git commit -m "Release v1.0.1"
git push origin main
```

### Semantic Versioning
- **Major (1.0.0)**: Breaking changes
- **Minor (1.1.0)**: New features, backward compatible
- **Patch (1.0.1)**: Bug fixes, backward compatible

## 📊 Package Contents

### What's Included in the Package
```
aws-node-termination-handler-1.0.0.tgz
├── Chart.yaml                    # Chart metadata
├── values.yaml                   # Default values
├── templates/                    # Kubernetes manifests
│   ├── daemonset.linux.yaml
│   ├── daemonset.windows.yaml
│   ├── deployment.yaml
│   ├── serviceaccount.yaml
│   ├── clusterrole.yaml
│   ├── clusterrolebinding.yaml
│   └── _helpers.tpl
├── README.md                     # Documentation
└── example/                      # Example configurations
```

### Index File Structure
```yaml
apiVersion: v1
entries:
  aws-node-termination-handler:
  - apiVersion: v2
    appVersion: "1.0.0"
    created: "2024-10-07T10:36:08.767329822+07:00"
    description: A Helm chart for the AWS Node Termination Handler.
    digest: sha256:abc123...
    home: https://github.com/aws/aws-node-termination-handler/
    icon: https://raw.githubusercontent.com/aws/eks-charts/master/docs/logo/aws.png
    keywords:
    - aws
    - eks
    - ec2
    - node-termination
    - spot
    kubeVersion: '>= 1.16-0'
    maintainers:
    - email: bwagner5@users.noreply.github.com
      name: Brandon Wagner
      url: https://github.com/bwagner5
    name: aws-node-termination-handler
    sources:
    - https://github.com/aws/aws-node-termination-handler/
    type: application
    urls:
    - https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/aws-node-termination-handler-1.0.0.tgz
    version: 1.0.0
generated: "2024-10-07T10:36:08.763268838+07:00"
```

## 🛠️ Troubleshooting

### Common Issues

#### 1. Package Creation Fails
```bash
# Check chart structure
helm lint .

# Verify Chart.yaml
cat Chart.yaml

# Test template generation
helm template test-release . --dry-run
```

#### 2. Index Generation Fails
```bash
# Check package exists
ls -la packages/*.tgz

# Verify URL format
helm repo index packages/ --url https://raw.githubusercontent.com/USER/REPO/main/path/
```

#### 3. Repository Not Found
```bash
# Verify GitHub URL is accessible
curl -I https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/index.yaml

# Check repository exists
helm repo add test https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/
helm repo update
```

#### 4. Chart Installation Fails
```bash
# Test with dry-run
helm install test-release banana-nth/aws-node-termination-handler --dry-run

# Check values
helm show values banana-nth/aws-node-termination-handler

# Debug template
helm template test-release banana-nth/aws-node-termination-handler
```

## 📚 Best Practices

### 1. Version Management
- Use semantic versioning (semver)
- Tag releases in Git
- Document changes in CHANGELOG.md

### 2. Chart Quality
- Run `helm lint` before packaging
- Test with `helm template` and `helm install --dry-run`
- Validate with different Kubernetes versions

### 3. Documentation
- Include comprehensive README.md
- Document all configuration options
- Provide usage examples

### 4. Security
- Use specific image tags (not `latest`)
- Include security contexts
- Follow Kubernetes security best practices

### 5. Testing
- Test on different Kubernetes distributions
- Validate with different configurations
- Test upgrade/downgrade scenarios

## 🎯 Usage Examples

### Add Repository
```bash
helm repo add banana-nth https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/
helm repo update
```

### Install Chart
```bash
# Basic installation
helm install aws-nth banana-nth/aws-node-termination-handler

# With custom values
helm install aws-nth banana-nth/aws-node-termination-handler \
  --set spotGuard.enabled=true \
  --set spotGuard.spotASGName=my-spot-asg \
  --set spotGuard.onDemandASGName=my-ondemand-asg

# With values file
helm install aws-nth banana-nth/aws-node-termination-handler -f my-values.yaml
```

### Upgrade Chart
```bash
# Upgrade to new version
helm upgrade aws-nth banana-nth/aws-node-termination-handler

# Upgrade with new values
helm upgrade aws-nth banana-nth/aws-node-termination-handler \
  --set spotGuard.enabled=true
```

### Uninstall Chart
```bash
helm uninstall aws-nth
```

## 📖 Additional Resources

- [Helm Documentation](https://helm.sh/docs/)
- [Helm Chart Best Practices](https://helm.sh/docs/chart_best_practices/)
- [Semantic Versioning](https://semver.org/)
- [Kubernetes Security Best Practices](https://kubernetes.io/docs/concepts/security/)

## 🔧 Automation Scripts

### Package and Index Script
```bash
#!/bin/bash
# package-and-index.sh

set -e

CHART_DIR="config/helm/aws-node-termination-handler"
PACKAGES_DIR="$CHART_DIR/packages"
REPO_URL="https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/"

echo "📦 Creating Helm package..."
cd $CHART_DIR
helm lint .
helm package .

echo "📋 Creating index..."
mkdir -p $PACKAGES_DIR
mv *.tgz $PACKAGES_DIR/
helm repo index $PACKAGES_DIR/ --url $REPO_URL

echo "✅ Package and index created successfully!"
echo "📁 Files created:"
ls -la $PACKAGES_DIR/

echo "🚀 Ready to commit and push to GitHub!"
```

### Usage
```bash
chmod +x package-and-index.sh
./package-and-index.sh
```

This guide provides everything needed to create, package, and distribute Helm charts via GitHub! 🎉
