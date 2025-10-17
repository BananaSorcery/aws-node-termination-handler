# IAM Setup Guide for AWS Node Termination Handler

This guide explains how to set up the required IAM role and policy for the AWS Node Termination Handler service account.

## Quick Start

### Using the Automated Script

The easiest way to set up IAM is using our provided script:

```bash
# Navigate to the IAM directory
cd config/helm/aws-node-termination-handler/iam

# Run the setup script with your cluster name
CLUSTER_NAME=your-eks-cluster-name ./setup-iam-role.sh
```

The script will output the role ARN that you need to add to your Helm values.

### Example Output

```
================================================
Setup Complete!
================================================

Use the following role ARN in your Helm values:

serviceAccount:
  name: aws-node-termination-handler
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/aws-node-termination-handler-spot-guard
```

## Custom Configuration

### Using Different Names or Namespaces

You can customize the role name, namespace, and other parameters:

```bash
CLUSTER_NAME=my-cluster \
ROLE_NAME=my-custom-role-name \
NAMESPACE=kube-system \
SERVICE_ACCOUNT_NAME=aws-node-termination-handler \
./setup-iam-role.sh
```

### For Multiple Namespaces

If you need the termination handler in multiple namespaces, you'll need to update the trust policy to include all namespaces:

```bash
# First namespace
CLUSTER_NAME=my-cluster NAMESPACE=namespace-1 ./setup-iam-role.sh

# Update for second namespace - this will update the trust policy
CLUSTER_NAME=my-cluster NAMESPACE=namespace-2 ROLE_NAME=aws-node-termination-handler-spot-guard ./setup-iam-role.sh
```

Or manually edit `trust_entities.json` to include multiple service accounts:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::ACCOUNT_ID:oidc-provider/oidc.eks.REGION.amazonaws.com/id/OIDC_ID"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "oidc.eks.REGION.amazonaws.com/id/OIDC_ID:aud": "sts.amazonaws.com"
        },
        "StringLike": {
          "oidc.eks.REGION.amazonaws.com/id/OIDC_ID:sub": [
            "system:serviceaccount:namespace-1:aws-node-termination-handler",
            "system:serviceaccount:namespace-2:aws-node-termination-handler"
          ]
        }
      }
    }
  ]
}
```

## Manual Setup

If you prefer to set up manually or need to understand what the script does:

### 1. Create IAM Policy

```bash
aws iam create-policy \
    --policy-name aws-node-termination-handler-policy \
    --policy-document file://policy.json \
    --description "Policy for AWS Node Termination Handler"
```

### 2. Get OIDC Provider

```bash
OIDC_PROVIDER=$(aws eks describe-cluster \
    --name YOUR_CLUSTER_NAME \
    --region YOUR_REGION \
    --query "cluster.identity.oidc.issuer" \
    --output text | sed -e "s/^https:\/\///")

echo $OIDC_PROVIDER
```

### 3. Create Trust Policy

Create a file `trust-policy.json`:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "Federated": "arn:aws:iam::YOUR_ACCOUNT_ID:oidc-provider/${OIDC_PROVIDER}"
            },
            "Action": "sts:AssumeRoleWithWebIdentity",
            "Condition": {
                "StringEquals": {
                    "${OIDC_PROVIDER}:aud": "sts.amazonaws.com",
                    "${OIDC_PROVIDER}:sub": "system:serviceaccount:YOUR_NAMESPACE:aws-node-termination-handler"
                }
            }
        }
    ]
}
```

### 4. Create IAM Role

```bash
aws iam create-role \
    --role-name aws-node-termination-handler-spot-guard \
    --assume-role-policy-document file://trust-policy.json \
    --description "IAM role for AWS Node Termination Handler"
```

### 5. Attach Policy to Role

```bash
aws iam attach-role-policy \
    --role-name aws-node-termination-handler-spot-guard \
    --policy-arn arn:aws:iam::YOUR_ACCOUNT_ID:policy/aws-node-termination-handler-policy
```

## IAM Permissions Explained

The `iam/policy.json` file grants the following permissions needed for Spot Guard functionality:

| Permission | Purpose |
|------------|---------|
| `autoscaling:DescribeAutoScalingGroups` | Query ASG details and current capacity |
| `autoscaling:DescribeScalingActivities` | Monitor scaling activities |
| `autoscaling:SetDesiredCapacity` | Adjust ASG capacity for pre-scaling |
| `autoscaling:DescribeAutoScalingInstances` | Get instance details in ASG |

## Verification

After setup, verify the role and policy:

```bash
# Verify role exists
aws iam get-role --role-name aws-node-termination-handler-spot-guard

# Verify policy is attached
aws iam list-attached-role-policies --role-name aws-node-termination-handler-spot-guard

# Verify trust relationship
aws iam get-role --role-name aws-node-termination-handler-spot-guard \
    --query 'Role.AssumeRolePolicyDocument'
```

## Troubleshooting

### OIDC Provider Not Found

If you get an error about OIDC provider:

1. Check if OIDC provider is enabled for your cluster:
   ```bash
   aws eks describe-cluster --name YOUR_CLUSTER --query "cluster.identity.oidc.issuer"
   ```

2. If not configured, enable it:
   ```bash
   eksctl utils associate-iam-oidc-provider --cluster YOUR_CLUSTER --approve
   ```

### Permission Denied

If pods can't assume the role:

1. Verify the service account annotation:
   ```bash
   kubectl get sa aws-node-termination-handler -n YOUR_NAMESPACE -o yaml
   ```

2. Check the trust policy matches your namespace and service account name

3. Verify OIDC provider ID in trust policy matches your cluster

### Policy Not Sufficient

If you see permission errors in pod logs, you may need additional permissions. Common additions:

- `ec2:DescribeInstances` - if using EC2 instance metadata
- `sqs:*` - if using queue processor mode
- Additional ASG permissions for advanced features

## Next Steps

After completing IAM setup:

1. Update your Helm values with the role ARN
2. Install or upgrade the Helm chart
3. Verify pods are running: `kubectl get pods -n YOUR_NAMESPACE`
4. Check pod logs for successful role assumption: `kubectl logs -n YOUR_NAMESPACE <pod-name>`

