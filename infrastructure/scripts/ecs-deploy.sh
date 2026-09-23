#!/usr/bin/env bash
# Deploys an image tag to ECS. Run by .github/workflows/deploy.yml.
#
#   ecs-deploy.sh migrate    run database migrations as a one-off task and wait for success
#   ecs-deploy.sh services   point every service at the new image and wait until stable
#
# Requires: AWS credentials, AWS_REGION, ECS_CLUSTER, ECR_REGISTRY, NAME_PREFIX, IMAGE_TAG.
set -euo pipefail

: "${ECS_CLUSTER:?}" "${ECR_REGISTRY:?}" "${NAME_PREFIX:?}" "${IMAGE_TAG:?}"

# Registers a new revision of a task definition family with one container's image replaced.
# Prints the new task definition ARN.
register() {
  local family="$1" image="$2"
  aws ecs describe-task-definition --task-definition "$family" --query taskDefinition --output json |
    jq --arg image "$image" '
      .containerDefinitions[0].image = $image
      | del(.taskDefinitionArn, .revision, .status, .requiresAttributes, .compatibilities, .registeredAt, .registeredBy)' \
      > /tmp/taskdef.json
  aws ecs register-task-definition --cli-input-json file:///tmp/taskdef.json --query taskDefinition.taskDefinitionArn --output text
}

case "${1:-}" in
  migrate)
    arn=$(register "$NAME_PREFIX-migrate" "$ECR_REGISTRY/$NAME_PREFIX-api:$IMAGE_TAG")
    # Reuse the API service's network settings: same subnets, same security group.
    network=$(aws ecs describe-services --cluster "$ECS_CLUSTER" --services "$NAME_PREFIX-api" \
      --query 'services[0].networkConfiguration' --output json)
    task=$(aws ecs run-task --cluster "$ECS_CLUSTER" --task-definition "$arn" --launch-type FARGATE \
      --network-configuration "$network" --query 'tasks[0].taskArn' --output text)
    echo "migration task: $task"
    aws ecs wait tasks-stopped --cluster "$ECS_CLUSTER" --tasks "$task"
    code=$(aws ecs describe-tasks --cluster "$ECS_CLUSTER" --tasks "$task" --query 'tasks[0].containers[0].exitCode' --output text)
    if [ "$code" != "0" ]; then
      echo "migrations failed with exit code $code; nothing has been rolled out" >&2
      exit 1
    fi
    ;;

  services)
    for service in api worker marketing admin; do
      # The worker runs the API image with a different command.
      image="$service"; [ "$service" = worker ] && image=api
      arn=$(register "$NAME_PREFIX-$service" "$ECR_REGISTRY/$NAME_PREFIX-$image:$IMAGE_TAG")
      aws ecs update-service --cluster "$ECS_CLUSTER" --service "$NAME_PREFIX-$service" --task-definition "$arn" > /dev/null
      echo "rolling out $service -> $arn"
    done
    # ECS circuit breakers roll a service back if the new tasks fail their health checks.
    aws ecs wait services-stable --cluster "$ECS_CLUSTER" \
      --services "$NAME_PREFIX-api" "$NAME_PREFIX-worker" "$NAME_PREFIX-marketing" "$NAME_PREFIX-admin"
    # Static pages are cached at the edge for a long time; a release replaces them.
    if [ -n "${CLOUDFRONT_DISTRIBUTION_ID:-}" ]; then
      aws cloudfront create-invalidation --distribution-id "$CLOUDFRONT_DISTRIBUTION_ID" --paths "/*" > /dev/null
    fi
    ;;

  *)
    echo "usage: ecs-deploy.sh migrate|services" >&2
    exit 2
    ;;
esac
