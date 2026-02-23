variable "service_name" {
  type        = string
  description = "Base name for ALB resources"
}
variable "vpc_id" {
  type        = string
  description = "VPC ID for the target group"
}
variable "subnet_ids" {
  type        = list(string)
  description = "Public subnets for the ALB"
}
variable "container_port" {
  type        = number
  description = "Port the containers listen on"
}
variable "alb_security_group_id" {
  type        = string
  description = "Security group ID for the ALB"
}
