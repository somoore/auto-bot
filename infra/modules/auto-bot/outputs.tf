output "app_url" {
  description = "Public URL for the app ALB."
  value       = var.app_domain_name != "" ? "${var.app_certificate_arn == "" ? "http" : "https"}://${var.app_domain_name}" : var.app_certificate_arn == "" ? "http://${aws_lb.app.dns_name}" : "https://${aws_lb.app.dns_name}"
}

output "app_alb_dns_name" {
  description = "App ALB DNS name."
  value       = aws_lb.app.dns_name
}

output "livekit_url" {
  description = "LiveKit Cloud URL used by the app."
  value       = var.livekit_cloud_url
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

output "cosign_kms_key_arn" {
  description = "KMS key ARN cosign signs and verifies the app image with (awskms://<arn>)."
  value       = aws_kms_key.cosign.arn
}

output "cosign_kms_key_alias" {
  description = "KMS alias for the cosign signing key."
  value       = aws_kms_alias.cosign.name
}

output "waf_web_acl_arn" {
  description = "AWS WAF web ACL attached to the app ALB."
  value       = aws_wafv2_web_acl.app.arn
}

# --- Edge auth outputs ---

output "acm_validation_records" {
  description = "DNS validation CNAME(s) to add at the domain's DNS provider (Cloudflare) so the ACM cert can validate. Set Cloudflare proxy to DNS-only."
  value = local.auth_enabled ? [
    for o in aws_acm_certificate.app[0].domain_validation_options : {
      name  = o.resource_record_name
      type  = o.resource_record_type
      value = o.resource_record_value
    }
  ] : []
}

output "app_dns_target" {
  description = "Create a CNAME from auth_domain_name to this ALB DNS name in your DNS provider (Cloudflare, DNS-only / grey cloud)."
  value       = aws_lb.app.dns_name
}

output "cognito_hosted_ui_domain" {
  description = "Cognito Hosted UI domain."
  value       = local.auth_enabled ? "${aws_cognito_user_pool_domain.this[0].domain}.auth.${var.aws_region}.amazoncognito.com" : ""
}

output "cognito_google_callback_url" {
  description = "Authorized redirect URI to register in the Google OAuth client (Google -> Cognito)."
  value       = local.auth_enabled ? "https://${aws_cognito_user_pool_domain.this[0].domain}.auth.${var.aws_region}.amazoncognito.com/oauth2/idpresponse" : ""
}

output "app_login_url" {
  description = "Public app URL behind Cognito login."
  value       = local.auth_enabled ? "https://${var.auth_domain_name}/" : ""
}

output "cloudwatch_dashboard_name" {
  description = "CloudWatch dashboard for app and ALB/WAF signals."
  value       = aws_cloudwatch_dashboard.ops.dashboard_name
}

output "board_efs_file_system_id" {
  description = "EFS file system backing the app's SQLite board store."
  value       = aws_efs_file_system.board.id
}
