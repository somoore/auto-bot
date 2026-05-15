output "app_url" {
  description = "Public URL for the app ALB."
  value       = var.app_domain_name != "" ? "${var.app_certificate_arn == "" ? "http" : "https"}://${var.app_domain_name}" : var.app_certificate_arn == "" ? "http://${aws_lb.app.dns_name}" : "https://${aws_lb.app.dns_name}"
}

output "app_alb_dns_name" {
  description = "App ALB DNS name."
  value       = aws_lb.app.dns_name
}

output "livekit_url" {
  description = "Browser/server LiveKit URL used by the app."
  value       = local.livekit_public_url
}

output "livekit_nlb_dns_name" {
  description = "LiveKit NLB DNS name."
  value       = try(aws_lb.livekit[0].dns_name, "")
}

output "livekit_deployment_mode" {
  description = "LiveKit deployment mode: self-hosted or cloud."
  value       = var.livekit_deployment_mode
}

output "livekit_redis_primary_endpoint" {
  description = "Primary Redis endpoint for self-hosted LiveKit distributed routing."
  value       = try(aws_elasticache_replication_group.livekit[0].primary_endpoint_address, "")
}

output "livekit_turn_url" {
  description = "Configured TURN endpoint for self-hosted LiveKit."
  value       = local.self_hosted_livekit && var.livekit_turn_domain_name != "" ? "turns:${var.livekit_turn_domain_name}:${var.livekit_turn_tls_port}" : ""
}

output "ecr_repository_url" {
  description = "ECR repository URL for the app image."
  value       = aws_ecr_repository.app.repository_url
}

output "ecs_cluster_name" {
  description = "ECS cluster name."
  value       = aws_ecs_cluster.this.name
}

output "private_subnet_ids" {
  description = "Private subnet IDs used by ECS tasks and EFS."
  value       = aws_subnet.private[*].id
}

output "public_subnet_ids" {
  description = "Public subnet IDs used only by load balancers and fck-nat."
  value       = aws_subnet.public[*].id
}

output "waf_web_acl_arn" {
  description = "AWS WAF web ACL attached to the app ALB."
  value       = aws_wafv2_web_acl.app.arn
}

output "cloudwatch_dashboard_name" {
  description = "CloudWatch dashboard for app, LiveKit, WAF, and Redis signals."
  value       = aws_cloudwatch_dashboard.ops.dashboard_name
}

output "app_service_name" {
  description = "ECS app service name."
  value       = aws_ecs_service.app.name
}

output "board_efs_file_system_id" {
  description = "EFS file system backing the app's SQLite board store."
  value       = aws_efs_file_system.board.id
}

output "livekit_service_name" {
  description = "ECS LiveKit service name."
  value       = try(aws_ecs_service.livekit[0].name, "")
}
