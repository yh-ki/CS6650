resource "aws_sns_topic" "orders" {
  name = "order-processing-events"
}

resource "aws_sqs_queue" "orders" {
  name                       = "order-processing-queue"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 345600  # 4 days
  receive_wait_time_seconds  = 20      # long polling
}

# Allow SNS to send messages to SQS
resource "aws_sqs_queue_policy" "orders" {
  queue_url = aws_sqs_queue.orders.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "sns.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.orders.arn
      Condition = {
        ArnEquals = { "aws:SourceArn" = aws_sns_topic.orders.arn }
      }
    }]
  })
}

# Subscribe SQS to SNS
resource "aws_sns_topic_subscription" "orders_sqs" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.orders.arn
}
