output "app_url" {
  description = "Public URL for the app ALB."
  value       = var.app_certificate_arn == "" ? "http://${aws_lb.app.dns_name}" : "https://${aws_lb.app.dns_name}"
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
  value       = aws_lb.livekit.dns_name
}

output "ecr_repository_url" {
  description = "ECR repository URL for the app image."
  value       = aws_ecr_repository.app.repository_url
}

output "ecs_cluster_name" {
  description = "ECS cluster name."
  value       = aws_ecs_cluster.this.name
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
  value       = aws_ecs_service.livekit.name
}
