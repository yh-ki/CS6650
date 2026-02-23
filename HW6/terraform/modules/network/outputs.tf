output "security_group_id" {
  value = aws_security_group.this.id
}
output "alb_security_group_id" {
  value = aws_security_group.alb.id
}
output "subnet_ids" {
  value = data.aws_subnets.default.ids
}
output "vpc_id" {
  value = data.aws_vpc.default.id
}
