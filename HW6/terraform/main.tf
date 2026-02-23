# Part III: Horizontal Scaling with ALB and Auto Scaling

module "network" {
  source         = "./modules/network"
  service_name   = var.service_name
  container_port = var.container_port
}

module "ecr" {
  source          = "./modules/ecr"
  repository_name = var.ecr_repository_name
}

module "logging" {
  source            = "./modules/logging"
  service_name      = var.service_name
  retention_in_days = var.log_retention_days
}

data "aws_iam_role" "lab_role" {
  name = "LabRole"
}

module "alb" {
  source                = "./modules/alb"
  service_name          = var.service_name
  vpc_id                = module.network.vpc_id
  subnet_ids            = module.network.subnet_ids
  container_port        = var.container_port
  alb_security_group_id = module.network.alb_security_group_id
}

resource "null_resource" "docker_build_push" {
  depends_on = [module.ecr]

  triggers = {
    main_go_hash = filemd5("../src/main.go")
  }

  provisioner "local-exec" {
    command = <<-EOT
      set -e
      cd ../src
      GOOS=linux GOARCH=amd64 go build -o server main.go
      aws ecr get-login-password --region ${var.aws_region} | \
        docker login --username AWS --password-stdin ${module.ecr.repository_url}
      docker buildx build --platform linux/amd64 --push \
        -t ${module.ecr.repository_url}:latest .
    EOT
  }
}

module "ecs" {
  source             = "./modules/ecs"
  service_name       = var.service_name
  image              = "${module.ecr.repository_url}:latest"
  container_port     = var.container_port
  subnet_ids         = module.network.subnet_ids
  security_group_ids = [module.network.security_group_id]
  execution_role_arn = data.aws_iam_role.lab_role.arn
  task_role_arn      = data.aws_iam_role.lab_role.arn
  log_group_name     = module.logging.log_group_name
  ecs_count          = 2
  region             = var.aws_region
  target_group_arn   = module.alb.target_group_arn

  depends_on = [null_resource.docker_build_push, module.alb]
}

# Auto Scaling target
resource "aws_appautoscaling_target" "ecs" {
  max_capacity       = 4
  min_capacity       = 2
  resource_id        = "service/${module.ecs.cluster_name}/${module.ecs.service_name}"
  scalable_dimension = "ecs:service:DesiredCount"
  service_namespace  = "ecs"
  depends_on         = [module.ecs]
}

# Scale out when average CPU > 70%
resource "aws_appautoscaling_policy" "cpu" {
  name               = "${var.service_name}-cpu-scaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}
