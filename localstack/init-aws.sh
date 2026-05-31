#!/bin/bash
set -e
awslocal sns create-topic --name payment-processed

awslocal sqs create-queue --queue-name order-pending
awslocal sqs create-queue --queue-name payment-processed-notification
awslocal sqs create-queue --queue-name payment-processed-inventory
awslocal sqs create-queue --queue-name payment-processed-analytics

TOPIC_ARN="arn:aws:sns:us-east-1:000000000000:payment-processed"

awslocal sns subscribe --topic-arn $TOPIC_ARN --protocol sqs \
  --notification-endpoint arn:aws:sqs:us-east-1:000000000000:payment-processed-notification

awslocal sns subscribe --topic-arn $TOPIC_ARN --protocol sqs \
  --notification-endpoint arn:aws:sqs:us-east-1:000000000000:payment-processed-inventory

awslocal sns subscribe --topic-arn $TOPIC_ARN --protocol sqs \
  --notification-endpoint arn:aws:sqs:us-east-1:000000000000:payment-processed-analytics

echo "LocalStack init complete."
