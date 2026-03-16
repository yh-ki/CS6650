#!/bin/bash
set -e

WORKERS=$1
REGION="us-west-2"
ACCOUNT="508444172391"
CLUSTER="hw7-cluster"
SERVICE="order-processor"
IMAGE="$ACCOUNT.dkr.ecr.$REGION.amazonaws.com/order-processor:latest"
LAB_ROLE="arn:aws:iam::$ACCOUNT:role/LabRole"

if [ -z "$WORKERS" ]; then
  echo "Usage: ./update-workers.sh <number_of_workers>"
  exit 1
fi

echo "Getting SQS queue URL..."
QUEUE_URL=$(aws sqs get-queue-url \
  --queue-name order-processing-queue \
  --region $REGION \
  --query QueueUrl \
  --output text)

echo "Registering new task definition with $WORKERS workers..."
aws ecs register-task-definition \
  --family $SERVICE \
  --network-mode awsvpc \
  --requires-compatibilities FARGATE \
  --cpu 256 \
  --memory 512 \
  --execution-role-arn $LAB_ROLE \
  --task-role-arn $LAB_ROLE \
  --region $REGION \
  --container-definitions "[
    {
      \"name\": \"order-processor\",
      \"image\": \"$IMAGE\",
      \"environment\": [
        {\"name\": \"SQS_QUEUE_URL\", \"value\": \"$QUEUE_URL\"},
        {\"name\": \"AWS_REGION\",    \"value\": \"$REGION\"},
        {\"name\": \"NUM_WORKERS\",   \"value\": \"$WORKERS\"}
      ],
      \"logConfiguration\": {
        \"logDriver\": \"awslogs\",
        \"options\": {
          \"awslogs-group\":         \"/ecs/order-processor\",
          \"awslogs-region\":        \"$REGION\",
          \"awslogs-stream-prefix\": \"ecs\"
        }
      },
      \"healthCheck\": {
        \"command\":     [\"CMD-SHELL\", \"wget -q -O- http://localhost:8081/health || exit 1\"],
        \"interval\":    30,
        \"timeout\":     5,
        \"retries\":     3,
        \"startPeriod\": 10
      }
    }
  ]" > /dev/null

echo "Getting latest task definition ARN..."
TASK_DEF=$(aws ecs describe-task-definition \
  --task-definition $SERVICE \
  --region $REGION \
  --query "taskDefinition.taskDefinitionArn" \
  --output text)

echo "Updating ECS service..."
aws ecs update-service \
  --cluster $CLUSTER \
  --service $SERVICE \
  --task-definition $TASK_DEF \
  --force-new-deployment \
  --region $REGION > /dev/null

echo "Done! order-processor is deploying with $WORKERS workers."
echo "Wait ~1 minute then check: aws ecs describe-services --cluster $CLUSTER --services $SERVICE --region $REGION --query 'services[*].{Running:runningCount,Desired:desiredCount}' --output table"
