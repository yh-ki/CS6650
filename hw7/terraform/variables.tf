variable "aws_region" {
  default = "us-west-2"
}

variable "project" {
  default = "hw7"
}

variable "lab_role_arn" {
  description = "Pre-existing LabRole ARN from AWS Academy"
  type        = string
}
