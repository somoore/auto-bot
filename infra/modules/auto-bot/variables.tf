variable "aws_region" {
  description = "AWS region for ECS, load balancer, CloudWatch, and Bedrock runtime access."
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix used for AWS resource names."
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for the dedicated auto-bot VPC this module creates. Two /24 public subnets are carved from it across two AZs."
  type        = string
  default     = "10.40.0.0/16"

  validation {
    condition     = can(cidrhost(var.vpc_cidr, 0))
    error_message = "vpc_cidr must be a valid IPv4 CIDR block such as 10.40.0.0/16."
  }
}

variable "allowed_ingress_cidrs" {
  description = "CIDR blocks allowed to reach the public app ALB."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "app_image" {
  description = "Fully qualified container image URI for the Go app. Leave empty only during the initial ECR bootstrap; deploys that scale the service above 0 must pass an immutable tag or digest."
  type        = string
  default     = ""

  validation {
    condition     = var.app_image == "" || !can(regex("(^|:)latest($|@)", var.app_image))
    error_message = "app_image must not use the latest tag."
  }
}

variable "app_desired_count" {
  description = "Number of app tasks to run. Default 0 for scale-to-zero; the fast-iteration helper scales 0<->1."
  type        = number
  default     = 0
}

variable "app_cpu" {
  description = "Fargate CPU units for the app task."
  type        = number
  default     = 512
}

variable "app_memory" {
  description = "Fargate memory MiB for the app task."
  type        = number
  default     = 1024
}

variable "voice_provider" {
  description = "Voice provider to run in the app task: openai or nova-sonic."
  type        = string
  default     = "nova-sonic"

  validation {
    condition     = contains(["openai", "nova-sonic"], var.voice_provider)
    error_message = "voice_provider must be openai or nova-sonic."
  }
}

variable "openai_realtime_model" {
  description = "OpenAI Realtime voice-to-action model for the OpenAI provider. Must support tool calling."
  type        = string
  default     = "gpt-realtime-2"
}

variable "openai_realtime_transcription_model" {
  description = "OpenAI streaming transcription model used inside the realtime meeting session."
  type        = string
  default     = "gpt-realtime-whisper"
}

variable "openai_realtime_translation_model" {
  description = "OpenAI dedicated live-translation model profile. Registered for capability discovery but not granted Jira/GitHub tools."
  type        = string
  default     = "gpt-realtime-translate"
}

variable "openai_realtime_translation_target_language" {
  description = "Default target language for the OpenAI realtime translation profile."
  type        = string
  default     = "en"
}

variable "nova_sonic_model" {
  description = "Amazon Bedrock Nova Sonic model ID."
  type        = string
  default     = "amazon.nova-2-sonic-v1:0"
}

variable "nova_sonic_voice" {
  description = "Nova Sonic TTS voice ID."
  type        = string
  default     = "matthew"
}

variable "audit_log_path" {
  description = "Container path for audit JSONL output. Leave empty to only log audit events to CloudWatch."
  type        = string
  default     = ""
}

variable "app_base_url" {
  description = "Optional public WebSocket URL override for the app, such as wss://app.example.com/websocket."
  type        = string
  default     = ""
}

variable "app_room_id" {
  description = "Single authorized LiveKit room ID for this deployment."
  type        = string
  default     = "kanban-meeting"
}

variable "app_board_id" {
  description = "Single authorized board ID for this deployment."
  type        = string
  default     = "default"
}

variable "livekit_cloud_url" {
  description = "LiveKit Cloud WebSocket URL, for example wss://project.livekit.cloud. Required: this module always uses LiveKit Cloud for the media plane."
  type        = string

  validation {
    condition     = can(regex("^wss?://", var.livekit_cloud_url))
    error_message = "livekit_cloud_url must be a ws:// or wss:// URL."
  }
}

variable "hosted_zone_id" {
  description = "Optional Route53 hosted zone ID for an app alias record."
  type        = string
  default     = ""
}

variable "app_domain_name" {
  description = "Optional DNS name for the app ALB, such as scrum.example.com."
  type        = string
  default     = ""
}

variable "app_certificate_arn" {
  description = "Optional pre-existing ACM certificate ARN for HTTPS on the app ALB. Leave empty when using auth_domain_name (the module then provisions its own cert). When both are empty the ALB serves plain HTTP (dev)."
  type        = string
  default     = ""
}

# --- Edge authentication (ALB + Cognito + Google) ---

variable "auth_domain_name" {
  description = "Public hostname for the app behind Cognito auth, e.g. meet.sc.tt. When set, the module provisions an ACM cert, a Cognito user pool with Google federation, and an ALB authenticate-cognito HTTPS listener. Empty disables edge auth (plain HTTP)."
  type        = string
  default     = ""
}

variable "cognito_domain_prefix" {
  description = "Prefix for the Cognito Hosted UI domain (<prefix>.auth.<region>.amazoncognito.com). Must be globally unique within the region."
  type        = string
  default     = ""
}

variable "google_client_id" {
  description = "Google OAuth 2.0 client ID for Cognito Google federation. Empty disables the Google IdP (only matters when auth_domain_name is set)."
  type        = string
  default     = ""
}

variable "google_client_secret" {
  description = "Google OAuth 2.0 client secret for Cognito Google federation. Sourced from Secrets Manager/env, never committed."
  type        = string
  default     = ""
  sensitive   = true
}

variable "host_emails" {
  description = "Comma-separated allowlist of email addresses granted the meeting host role after Cognito login. Everyone else who logs in is a participant."
  type        = string
  default     = ""
}

variable "allowed_emails" {
  description = "Comma-separated allowlist of exact email addresses permitted to access the app after Cognito login. Combined with allowed_email_domains. When both are empty, any authenticated Google user is allowed."
  type        = string
  default     = ""
}

variable "allowed_email_domains" {
  description = "Comma-separated allowlist of email domains (e.g. moore.cloud) permitted to access the app after Cognito login. Authenticated users outside the allowlist are denied by the app."
  type        = string
  default     = ""
}

variable "auth_bypass_path_patterns" {
  description = "ALB path patterns exempted from Cognito auth (machine callbacks that cannot do an OIDC browser login, e.g. webhooks). They rely on in-app signature/secret validation."
  type        = list(string)
  default     = ["/jira/webhook", "/healthz"]
}

variable "verbose_logging" {
  description = "Enable pion Info-level logging (PION_LOG_INFO=all) for debugging the voice agent lifecycle. Turn off in steady state."
  type        = bool
  default     = false
}

variable "enable_alb_access_logs" {
  description = "Enable ALB access logs to an S3 bucket for debugging authenticate-cognito failures (exposes the error_reason field). Turn off when not debugging."
  type        = bool
  default     = false
}

variable "acm_wait_for_validation" {
  description = "When true, terraform waits for the ACM DNS validation record to be live before completing. Set false on the first apply (before the CNAME is added to DNS), then true once the record is in place."
  type        = bool
  default     = false
}

variable "waf_rate_limit" {
  description = "Per-IP five-minute request rate limit enforced by AWS WAF on the app ALB. Tuned low for a small allowlisted audience."
  type        = number
  default     = 600
}

variable "enable_bot_control" {
  description = "Enable the AWS WAF Bot Control managed rule group (behavioral bot detection). Adds WAF request cost; off by default."
  type        = bool
  default     = false
}

variable "app_api_token_secret_arn" {
  description = "Secrets Manager ARN containing APP_API_TOKEN."
  type        = string

  validation {
    condition     = var.app_api_token_secret_arn != ""
    error_message = "app_api_token_secret_arn is required."
  }
}

variable "livekit_api_key_secret_arn" {
  description = "Secrets Manager ARN containing the LiveKit Cloud LIVEKIT_API_KEY."
  type        = string

  validation {
    condition     = var.livekit_api_key_secret_arn != ""
    error_message = "livekit_api_key_secret_arn is required."
  }
}

variable "livekit_api_secret_secret_arn" {
  description = "Secrets Manager ARN containing the LiveKit Cloud LIVEKIT_API_SECRET."
  type        = string

  validation {
    condition     = var.livekit_api_secret_secret_arn != ""
    error_message = "livekit_api_secret_secret_arn is required."
  }
}

variable "openai_api_key_secret_arn" {
  description = "Optional Secrets Manager ARN containing OPENAI_API_KEY."
  type        = string
  default     = ""
}

variable "jira_api_token_secret_arn" {
  description = "Optional Secrets Manager ARN containing JIRA_API_TOKEN."
  type        = string
  default     = ""
}

variable "jira_config_json_secret_arn" {
  description = "Optional Secrets Manager ARN containing JIRA_CONFIG_JSON."
  type        = string
  default     = ""
}

variable "jira_webhook_secret_secret_arn" {
  description = "Optional Secrets Manager ARN containing JIRA_WEBHOOK_SECRET for Jira webhook authentication."
  type        = string
  default     = ""
}

variable "github_app_id_secret_arn" {
  description = "Optional Secrets Manager ARN containing GITHUB_APP_ID for autonomous agent GitHub App access."
  type        = string
  default     = ""
}

variable "github_app_installation_id_secret_arn" {
  description = "Optional Secrets Manager ARN containing GITHUB_APP_INSTALLATION_ID for autonomous agent GitHub App access."
  type        = string
  default     = ""
}

variable "github_app_private_key_secret_arn" {
  description = "Optional Secrets Manager ARN containing the PEM GITHUB_APP_PRIVATE_KEY for autonomous agent GitHub App access."
  type        = string
  default     = ""
}

variable "github_default_repo" {
  description = "Optional default GitHub repo in owner/name form for autonomous agent runs."
  type        = string
  default     = ""
}

variable "github_allowed_repos" {
  description = "Comma-separated allowlist of GitHub repos in owner/name form. Agent GitHub access refuses repos outside this list."
  type        = string
  default     = ""
}

variable "github_pr_comments_enabled" {
  description = "Enable autonomous PR review comments. Requires the GitHub App to have Pull requests: write."
  type        = bool
  default     = false
}

variable "agent_pm_model" {
  description = "AWS Bedrock model ID used by the project-manager agent for classification."
  type        = string
  default     = "us.anthropic.claude-haiku-4-5-20251001-v1:0"
}

variable "agent_review_model" {
  description = "AWS Bedrock model ID used by the code-review specialist."
  type        = string
  default     = "us.anthropic.claude-sonnet-4-6"
}

variable "bedrock_model_arns" {
  description = "Bedrock model ARNs the app task may invoke."
  type        = list(string)
  default = [
    "arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-2-sonic-v1:0",
    "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-haiku-4-5-20251001-v1:0",
    "arn:aws:bedrock:us-east-2::foundation-model/anthropic.claude-haiku-4-5-20251001-v1:0",
    "arn:aws:bedrock:us-west-2::foundation-model/anthropic.claude-haiku-4-5-20251001-v1:0",
    "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-sonnet-4-6",
    "arn:aws:bedrock:us-east-2::foundation-model/anthropic.claude-sonnet-4-6",
    "arn:aws:bedrock:us-west-2::foundation-model/anthropic.claude-sonnet-4-6",
    "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-opus-4-5-20251101-v1:0",
    "arn:aws:bedrock:us-east-2::foundation-model/anthropic.claude-opus-4-5-20251101-v1:0",
    "arn:aws:bedrock:us-west-2::foundation-model/anthropic.claude-opus-4-5-20251101-v1:0",
    "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-opus-4-6-v1",
    "arn:aws:bedrock:us-east-2::foundation-model/anthropic.claude-opus-4-6-v1",
    "arn:aws:bedrock:us-west-2::foundation-model/anthropic.claude-opus-4-6-v1",
    "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-opus-4-7",
    "arn:aws:bedrock:us-east-2::foundation-model/anthropic.claude-opus-4-7",
    "arn:aws:bedrock:us-west-2::foundation-model/anthropic.claude-opus-4-7",
  ]

  validation {
    condition     = !contains(var.bedrock_model_arns, "*")
    error_message = "bedrock_model_arns must be narrowed to explicit model ARNs."
  }
}

variable "log_retention_days" {
  description = "CloudWatch log retention in days."
  type        = number
  default     = 14
}
