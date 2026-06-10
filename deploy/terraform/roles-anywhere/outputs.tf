# Paste these into the Helm chart's awsRolesAnywhere.* values.

output "trust_anchor_arn" {
  description = "Set as awsRolesAnywhere.trustAnchorArn"
  value       = aws_rolesanywhere_trust_anchor.this.arn
}

output "profile_arn" {
  description = "Set as awsRolesAnywhere.profileArn"
  value       = aws_rolesanywhere_profile.this.arn
}

output "role_arn" {
  description = "Set as awsRolesAnywhere.roleArn"
  value       = aws_iam_role.bedrock.arn
}
