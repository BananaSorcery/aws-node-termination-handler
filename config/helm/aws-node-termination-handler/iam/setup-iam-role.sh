#!/bin/bash

# AWS Node Termination Handler - IAM Role Setup Script
# This script creates the necessary IAM role and policy for the service account

set -e

# Configuration
ROLE_NAME="${ROLE_NAME:-aws-node-termination-handler-spot-guard}"
POLICY_NAME="${POLICY_NAME:-aws-node-termination-handler-policy}"
AWS_ACCOUNT_ID="${AWS_ACCOUNT_ID:-$(aws sts get-caller-identity --query Account --output text)}"
AWS_REGION="${AWS_REGION:-us-west-2}"
CLUSTER_NAME="${CLUSTER_NAME:-}"
NAMESPACE="${NAMESPACE:-mdaas-engines-dev}"
SERVICE_ACCOUNT_NAME="${SERVICE_ACCOUNT_NAME:-aws-node-termination-handler}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}================================================${NC}"
echo -e "${GREEN}AWS Node Termination Handler - IAM Setup${NC}"
echo -e "${GREEN}================================================${NC}"
echo ""

# Validate required parameters
if [ -z "$CLUSTER_NAME" ]; then
    echo -e "${RED}Error: CLUSTER_NAME is required${NC}"
    echo "Usage: CLUSTER_NAME=your-cluster-name ./setup-iam-role.sh"
    echo ""
    echo "Optional environment variables:"
    echo "  ROLE_NAME            - IAM role name (default: aws-node-termination-handler-spot-guard)"
    echo "  POLICY_NAME          - IAM policy name (default: aws-node-termination-handler-policy)"
    echo "  AWS_ACCOUNT_ID       - AWS Account ID (auto-detected if not provided)"
    echo "  AWS_REGION           - AWS Region (default: us-west-2)"
    echo "  NAMESPACE            - Kubernetes namespace (default: mdaas-engines-dev)"
    echo "  SERVICE_ACCOUNT_NAME - Service account name (default: aws-node-termination-handler)"
    exit 1
fi

echo -e "Configuration:"
echo -e "  AWS Account ID:       ${YELLOW}${AWS_ACCOUNT_ID}${NC}"
echo -e "  AWS Region:           ${YELLOW}${AWS_REGION}${NC}"
echo -e "  Cluster Name:         ${YELLOW}${CLUSTER_NAME}${NC}"
echo -e "  Namespace:            ${YELLOW}${NAMESPACE}${NC}"
echo -e "  Service Account:      ${YELLOW}${SERVICE_ACCOUNT_NAME}${NC}"
echo -e "  IAM Role Name:        ${YELLOW}${ROLE_NAME}${NC}"
echo -e "  IAM Policy Name:      ${YELLOW}${POLICY_NAME}${NC}"
echo ""

# Get OIDC provider for the cluster
echo -e "${GREEN}[1/5]${NC} Getting OIDC provider for cluster..."
OIDC_PROVIDER=$(aws eks describe-cluster --name "$CLUSTER_NAME" --region "$AWS_REGION" --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")

if [ -z "$OIDC_PROVIDER" ]; then
    echo -e "${RED}Error: Could not get OIDC provider for cluster ${CLUSTER_NAME}${NC}"
    exit 1
fi

echo -e "  OIDC Provider: ${YELLOW}${OIDC_PROVIDER}${NC}"
echo ""

# Create trust policy document
echo -e "${GREEN}[2/5]${NC} Creating trust policy document..."
TRUST_POLICY=$(cat <<EOF
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "Federated": "arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/${OIDC_PROVIDER}"
            },
            "Action": "sts:AssumeRoleWithWebIdentity",
            "Condition": {
                "StringEquals": {
                    "${OIDC_PROVIDER}:aud": "sts.amazonaws.com",
                    "${OIDC_PROVIDER}:sub": "system:serviceaccount:${NAMESPACE}:${SERVICE_ACCOUNT_NAME}"
                }
            }
        }
    ]
}
EOF
)

echo "$TRUST_POLICY" > /tmp/trust-policy.json
echo -e "  Trust policy saved to: ${YELLOW}/tmp/trust-policy.json${NC}"
echo ""

# Create IAM policy
echo -e "${GREEN}[3/5]${NC} Creating IAM policy..."

# Check if policy.json exists
if [ ! -f "policy.json" ]; then
    echo -e "${RED}Error: policy.json not found in current directory${NC}"
    exit 1
fi

# Check if policy already exists
EXISTING_POLICY_ARN=$(aws iam list-policies --scope Local --query "Policies[?PolicyName=='${POLICY_NAME}'].Arn" --output text 2>/dev/null || echo "")

if [ -n "$EXISTING_POLICY_ARN" ]; then
    echo -e "  Policy already exists: ${YELLOW}${EXISTING_POLICY_ARN}${NC}"
    POLICY_ARN="$EXISTING_POLICY_ARN"
else
    POLICY_ARN=$(aws iam create-policy \
        --policy-name "$POLICY_NAME" \
        --policy-document file://policy.json \
        --description "Policy for AWS Node Termination Handler to manage ASG and EC2 instances" \
        --query 'Policy.Arn' \
        --output text)
    
    echo -e "  Policy created: ${GREEN}${POLICY_ARN}${NC}"
fi
echo ""

# Create IAM role
echo -e "${GREEN}[4/5]${NC} Creating IAM role..."

# Check if role already exists
if aws iam get-role --role-name "$ROLE_NAME" >/dev/null 2>&1; then
    echo -e "  ${YELLOW}Role already exists, updating trust policy...${NC}"
    aws iam update-assume-role-policy \
        --role-name "$ROLE_NAME" \
        --policy-document file:///tmp/trust-policy.json
    echo -e "  Trust policy updated"
else
    aws iam create-role \
        --role-name "$ROLE_NAME" \
        --assume-role-policy-document file:///tmp/trust-policy.json \
        --description "IAM role for AWS Node Termination Handler service account"
    echo -e "  ${GREEN}Role created: ${ROLE_NAME}${NC}"
fi

ROLE_ARN="arn:aws:iam::${AWS_ACCOUNT_ID}:role/${ROLE_NAME}"
echo ""

# Attach policy to role
echo -e "${GREEN}[5/5]${NC} Attaching policy to role..."

# Check if policy is already attached
ATTACHED=$(aws iam list-attached-role-policies --role-name "$ROLE_NAME" --query "AttachedPolicies[?PolicyArn=='${POLICY_ARN}'].PolicyArn" --output text 2>/dev/null || echo "")

if [ -n "$ATTACHED" ]; then
    echo -e "  Policy already attached to role"
else
    aws iam attach-role-policy \
        --role-name "$ROLE_NAME" \
        --policy-arn "$POLICY_ARN"
    echo -e "  ${GREEN}Policy attached successfully${NC}"
fi
echo ""

# Clean up
rm -f /tmp/trust-policy.json

echo -e "${GREEN}================================================${NC}"
echo -e "${GREEN}Setup Complete!${NC}"
echo -e "${GREEN}================================================${NC}"
echo ""
echo -e "Use the following role ARN in your Helm values:"
echo ""
echo -e "${YELLOW}serviceAccount:${NC}"
echo -e "${YELLOW}  name: ${SERVICE_ACCOUNT_NAME}${NC}"
echo -e "${YELLOW}  annotations:${NC}"
echo -e "${YELLOW}    eks.amazonaws.com/role-arn: ${ROLE_ARN}${NC}"
echo ""
echo -e "Or set it in your ArgoCD application:"
echo ""
echo -e "${YELLOW}  helm:${NC}"
echo -e "${YELLOW}    values: |-${NC}"
echo -e "${YELLOW}      serviceAccount:${NC}"
echo -e "${YELLOW}        name: ${SERVICE_ACCOUNT_NAME}${NC}"
echo -e "${YELLOW}        annotations:${NC}"
echo -e "${YELLOW}          eks.amazonaws.com/role-arn: ${ROLE_ARN}${NC}"
echo ""

