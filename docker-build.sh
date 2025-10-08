#!/bin/bash

# Set your variables
export IMG=043701111869.dkr.ecr.us-west-2.amazonaws.com/aws-node-termination-handler
export IMG_TAG=compatible-1

# Build v1-compatible image
make docker-build-v1

# Tag and push
docker tag ${IMG}:${IMG_TAG}-v1 ${IMG}:${IMG_TAG}
aws ecr get-login-password --region us-west-2 | \
  docker login --username AWS --password-stdin 043701111869.dkr.ecr.us-west-2.amazonaws.com
docker push ${IMG}:${IMG_TAG}