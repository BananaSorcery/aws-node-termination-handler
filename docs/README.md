# 📚 AWS Node Termination Handler Documentation

Welcome to the AWS Node Termination Handler documentation!

---

## 📂 Documentation Sections

### 🎯 [Spot Guard Documentation](./spot-guard/)
Comprehensive guides for the Spot Guard feature - automatic cost optimization for AWS Spot Instances.

**Key Topics:**
- Complete flow diagrams (v1.0 and v2.0)
- Architecture deep-dives
- Capacity check improvements
- Migration guides
- Deployment checklists

**Quick Links:**
- [🚀 Complete Flow v2.0](./spot-guard/SPOT_GUARD_COMPLETE_FLOW_V2.md) - **Start here!**
- [📊 v1.0 vs v2.0 Comparison](./spot-guard/SPOT_GUARD_V1_VS_V2_COMPARISON.md)
- [🏗️ Drain Architecture](./spot-guard/SPOT_GUARD_DRAIN_ARCHITECTURE.md)

---

## 🎯 What is AWS Node Termination Handler?

AWS Node Termination Handler gracefully handles EC2 instance interruptions to ensure your Kubernetes workloads continue running smoothly. It monitors for:

- 🔴 **Spot Instance Interruption Notices** (2-minute warning)
- 🔄 **Rebalance Recommendations** (proactive notifications)
- 🔧 **Scheduled Maintenance Events** (planned AWS maintenance)
- 📦 **ASG Lifecycle Hooks** (instance termination)

---

## 🌟 Feature Highlights

### **Core Features:**
- ✅ Graceful node draining
- ✅ PodDisruptionBudget respect
- ✅ Kubernetes event emission
- ✅ Webhook notifications
- ✅ SQS-based monitoring

### **Spot Guard (v2.0):**
- ✅ Automatic on-demand scale-down
- ✅ Smart pre-scale (3-level fallback)
- ✅ CA protection for spot nodes
- ✅ Zero false positives
- ✅ 95%+ cost reduction

---

## 📖 Quick Start

### Installation via Helm:

```bash
# Add the helm repository
helm repo add eks https://aws.github.io/eks-charts
helm repo update

# Install with Spot Guard enabled
helm install aws-node-termination-handler eks/aws-node-termination-handler \
  --namespace kube-system \
  --set spotGuard.enabled=true \
  --set spotGuard.spotASGName=my-spot-asg \
  --set spotGuard.onDemandASGName=my-on-demand-asg \
  --set spotGuard.enablePreScale=true
```

See [Spot Guard Deployment Checklist](./spot-guard/SPOT_GUARD_COMPLETE_FLOW_V2.md#-deployment-checklist) for full configuration.

---

## 🗂️ Repository Structure

```
aws-node-termination-handler/
├── cmd/                          # Main application entry point
├── pkg/                          # Core packages
│   ├── spotguard/               # Spot Guard implementation
│   ├── node/                    # Node management (drain, cordon)
│   ├── monitor/                 # Event monitors
│   └── ...
├── config/helm/                 # Helm charts
├── docs/                        # Documentation (you are here!)
│   └── spot-guard/             # Spot Guard documentation
├── testing/                     # Test scripts and guides
└── ...
```

---

## 🤝 Contributing

Contributions are welcome! Please:
1. Read the documentation in this folder
2. Check existing issues
3. Submit PRs with tests and documentation

---

## 📞 Support

For questions or issues:
- **General NTH**: Check main repository issues
- **Spot Guard**: See [Spot Guard Documentation](./spot-guard/)

---

**Last Updated:** 2025-10-14



