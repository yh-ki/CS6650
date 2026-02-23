output "alb_dns_name" {
  value       = module.alb.alb_dns_name
  description = "Use this URL to send requests in Part III (instead of task IP)"
}
output "ecs_cluster_name" {
  value = module.ecs.cluster_name
}
output "ecs_service_name" {
  value = module.ecs.service_name
}
