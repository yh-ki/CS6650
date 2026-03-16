output "alb_dns_name" {
  value = aws_lb.main.dns_name
}

output "sns_topic_arn" {
  value = aws_sns_topic.orders.arn
}

output "sqs_queue_url" {
  value = aws_sqs_queue.orders.url
}

output "ecr_receiver_url" {
  value = aws_ecr_repository.order_receiver.repository_url
}

output "ecr_processor_url" {
  value = aws_ecr_repository.order_processor.repository_url
}
