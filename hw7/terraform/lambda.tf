resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/order-processor-lambda"
  retention_in_days = 7
}

resource "aws_lambda_function" "order_processor" {
  function_name = "order-processor-lambda"
  role          = var.lab_role_arn
  runtime       = "provided.al2"
  handler       = "bootstrap"
  filename      = "${path.module}/lambda.zip"
  memory_size   = 512
  timeout       = 30

  environment {
    variables = {
      AWS_REGION_NAME = var.aws_region
    }
  }

  depends_on = [aws_cloudwatch_log_group.lambda]
}

resource "aws_lambda_permission" "sns" {
  statement_id  = "AllowSNSInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.order_processor.function_name
  principal     = "sns.amazonaws.com"
  source_arn    = aws_sns_topic.orders.arn
}

resource "aws_sns_topic_subscription" "lambda" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "lambda"
  endpoint  = aws_lambda_function.order_processor.arn
}

output "lambda_function_name" {
  value = aws_lambda_function.order_processor.function_name
}
