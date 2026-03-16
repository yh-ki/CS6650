resource "aws_ecs_cluster" "main" {
  name = "${var.project}-cluster"
}

# CloudWatch log groups
resource "aws_cloudwatch_log_group" "order_receiver" {
  name              = "/ecs/order-receiver"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "order_processor" {
  name              = "/ecs/order-processor"
  retention_in_days = 7
}

# ECR repos
resource "aws_ecr_repository" "order_receiver" {
  name         = "order-receiver"
  force_delete = true
}

resource "aws_ecr_repository" "order_processor" {
  name         = "order-processor"
  force_delete = true
}

# Order Receiver task definition
resource "aws_ecs_task_definition" "order_receiver" {
  family                   = "order-receiver"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = var.lab_role_arn
  task_role_arn            = var.lab_role_arn

  container_definitions = jsonencode([{
    name  = "order-receiver"
    image = "${aws_ecr_repository.order_receiver.repository_url}:latest"
    portMappings = [{ containerPort = 8080, protocol = "tcp" }]
    environment = [
      { name = "SNS_TOPIC_ARN", value = aws_sns_topic.orders.arn },
      { name = "AWS_REGION",    value = var.aws_region }
    ]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = "/ecs/order-receiver"
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "ecs"
      }
    }
    healthCheck = {
      command     = ["CMD-SHELL", "wget -q -O- http://localhost:8080/health || exit 1"]
      interval    = 30
      timeout     = 5
      retries     = 3
      startPeriod = 10
    }
  }])
}

# Order Processor task definition
resource "aws_ecs_task_definition" "order_processor" {
  family                   = "order-processor"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = var.lab_role_arn
  task_role_arn            = var.lab_role_arn

  container_definitions = jsonencode([{
    name  = "order-processor"
    image = "${aws_ecr_repository.order_processor.repository_url}:latest"
    environment = [
      { name = "SQS_QUEUE_URL", value = aws_sqs_queue.orders.url },
      { name = "AWS_REGION",    value = var.aws_region },
      { name = "NUM_WORKERS",   value = "1" }
    ]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = "/ecs/order-processor"
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "ecs"
      }
    }
    healthCheck = {
      command     = ["CMD-SHELL", "wget -q -O- http://localhost:8081/health || exit 1"]
      interval    = 30
      timeout     = 5
      retries     = 3
      startPeriod = 10
    }
  }])
}

# Security groups
resource "aws_security_group" "alb" {
  name   = "${var.project}-alb-sg"
  vpc_id = aws_vpc.main.id
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "ecs" {
  name   = "${var.project}-ecs-sg"
  vpc_id = aws_vpc.main.id
  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ALB
resource "aws_lb" "main" {
  name               = "${var.project}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = [aws_subnet.public_1.id, aws_subnet.public_2.id]
}

resource "aws_lb_target_group" "order_receiver" {
  name        = "${var.project}-order-tg"
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = aws_vpc.main.id
  target_type = "ip"
  health_check {
    path                = "/health"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 30
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"
  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.order_receiver.arn
  }
}

# ECS Services
resource "aws_ecs_service" "order_receiver" {
  name            = "order-receiver"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.order_receiver.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets         = [aws_subnet.private_1.id, aws_subnet.private_2.id]
    security_groups = [aws_security_group.ecs.id]
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.order_receiver.arn
    container_name   = "order-receiver"
    container_port   = 8080
  }

  depends_on = [aws_lb_listener.http]
}

resource "aws_ecs_service" "order_processor" {
  name            = "order-processor"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.order_processor.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets         = [aws_subnet.private_1.id, aws_subnet.private_2.id]
    security_groups = [aws_security_group.ecs.id]
  }
}
