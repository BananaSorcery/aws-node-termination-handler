# IAM Configuration

This directory contains all IAM-related files and scripts for setting up AWS Node Termination Handler with Spot Guard.

## Files

- **`setup-iam-role.sh`** - Automated script to create IAM role and policy
- **`policy.json`** - IAM policy with required permissions for Spot Guard
- **`trust_entities.json`** - Example trust policy for the IAM role
- **`IAM-SETUP.md`** - Comprehensive setup guide

## Quick Start

Run the setup script with your cluster name:

```bash
CLUSTER_NAME=your-cluster-name ./setup-iam-role.sh
```

For detailed instructions, see [IAM-SETUP.md](IAM-SETUP.md).

## What Gets Created

The setup script creates:
1. **IAM Policy** - Grants permissions to manage Auto Scaling Groups
2. **IAM Role** - For the Kubernetes service account to assume
3. **Trust Relationship** - Allows the service account to use the role via OIDC

## Required Permissions

The policy grants these permissions:
- `autoscaling:DescribeAutoScalingGroups`
- `autoscaling:DescribeScalingActivities`
- `autoscaling:SetDesiredCapacity`
- `autoscaling:DescribeAutoScalingInstances`

## After Setup

Add the role ARN to your Helm values:

```yaml
serviceAccount:
  name: aws-node-termination-handler
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT_ID:role/ROLE_NAME
```

