variable "aws_region" {
  type    = string
  default = "us-west-2"
}
variable "service_name" {
  type    = string
  default = "product-search"
}
variable "container_port" {
  type    = number
  default = 8080
}
variable "ecr_repository_name" {
  type    = string
  default = "ecr_service"
}
variable "log_retention_days" {
  type    = number
  default = 7
}
variable "ecs_count" {
  type    = number
  default = 2
}
