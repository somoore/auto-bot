variable "aws_region" {
  description = "AWS region for ECS, load balancers, CloudWatch, and Bedrock runtime access."
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix used for AWS resource names."
  type        = string
}

variable "vpc_cidr" {
  description = "Canonical VPC CIDR block for the requested 10.20.21.0/16 range. AWS canonicalizes that non-network address to 10.20.0.0/16."
  type        = string
  default     = "10.20.0.0/16"
}

variable "allowed_ingress_cidrs" {
  description = "CIDR blocks allowed to reach the public app and LiveKit endpoints."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "app_image" {
  description = "Fully qualified container image URI for the Go app. Leave empty only during the initial ECR bootstrap; full deploys should pass an immutable tag or digest."
  type        = string
  default     = ""

  validation {
    condition     = var.app_image == "" || !can(regex("(^|:)latest($|@)", var.app_image))
    error_message = "app_image must not use the latest tag."
  }
}

variable "app_desired_count" {
  description = "Number of app tasks to run."
  type        = number
  default     = 1
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
  description = "OpenAI Realtime model for the OpenAI provider."
  type        = string
  default     = "gpt-realtime-2"
}

variable "nova_sonic_model" {
  description = "Amazon Bedrock Nova Sonic model ID."
  type        = string
  default     = "amazon.nova-sonic-v1:0"
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

variable "livekit_image" {
  description = "LiveKit server container image."
  type        = string
  default     = "livekit/livekit-server:v1.9.1@sha256:c039a1bfa154c8479ac369c380665638e92a7e9531e69664549c0c0d3eb65e63"

  validation {
    condition     = can(regex("@sha256:[0-9a-f]{64}$", var.livekit_image)) && !can(regex("(^|:)latest($|@)", var.livekit_image))
    error_message = "livekit_image must be pinned to a sha256 digest and must not use the latest tag."
  }
}

variable "livekit_deployment_mode" {
  description = "LiveKit media plane mode: self-hosted deploys LiveKit in this stack; cloud uses LiveKit Cloud with the provided URL and secrets."
  type        = string
  default     = "self-hosted"

  validation {
    condition     = contains(["self-hosted", "cloud"], var.livekit_deployment_mode)
    error_message = "livekit_deployment_mode must be self-hosted or cloud."
  }
}

variable "livekit_cloud_url" {
  description = "LiveKit Cloud WebSocket URL, for example wss://project.livekit.cloud. Required when livekit_deployment_mode is cloud."
  type        = string
  default     = ""
}

variable "livekit_desired_count" {
  description = "Number of self-hosted LiveKit tasks. Redis-backed distributed mode supports multiple tasks; a single room still fits on one node."
  type        = number
  default     = 2
}

variable "livekit_cpu" {
  description = "Fargate CPU units for the LiveKit task."
  type        = number
  default     = 1024
}

variable "livekit_memory" {
  description = "Fargate memory MiB for the LiveKit task."
  type        = number
  default     = 2048
}

variable "livekit_signal_port" {
  description = "LiveKit HTTP/WebSocket signaling port."
  type        = number
  default     = 7880
}

variable "livekit_public_signal_port" {
  description = "Public NLB listener port for LiveKit signaling. Use 0 to default to 443 when TLS is enabled, otherwise livekit_signal_port."
  type        = number
  default     = 0
}

variable "livekit_tcp_port" {
  description = "LiveKit TCP fallback port for RTC."
  type        = number
  default     = 7881
}

variable "livekit_udp_port" {
  description = "Muxed UDP RTC media port exposed by the LiveKit task."
  type        = number
  default     = 7882
}

variable "livekit_prometheus_port" {
  description = "Private LiveKit Prometheus metrics port exposed inside the task."
  type        = number
  default     = 6789
}

variable "livekit_turn_enabled" {
  description = "Enable embedded LiveKit TURN for self-hosted deployments."
  type        = bool
  default     = true
}

variable "livekit_turn_udp_enabled" {
  description = "Expose embedded TURN over UDP through the LiveKit NLB."
  type        = bool
  default     = true
}

variable "livekit_turn_tls_enabled" {
  description = "Expose embedded TURN/TLS through the LiveKit NLB. Requires livekit_turn_certificate_arn and livekit_turn_domain_name."
  type        = bool
  default     = false
}

variable "livekit_turn_udp_port" {
  description = "Public and target UDP port for embedded TURN/UDP."
  type        = number
  default     = 443
}

variable "livekit_turn_tls_port" {
  description = "Public and target TCP/TLS port for embedded TURN/TLS."
  type        = number
  default     = 5349
}

variable "livekit_turn_domain_name" {
  description = "DNS name advertised for embedded TURN, such as turn.example.com."
  type        = string
  default     = ""
}

variable "livekit_url_override" {
  description = "Optional browser-facing LiveKit URL, such as wss://livekit.example.com. Defaults to ws://<nlb-dns>:7880."
  type        = string
  default     = ""
}

variable "hosted_zone_id" {
  description = "Optional Route53 hosted zone ID for app, LiveKit, and TURN alias records."
  type        = string
  default     = ""
}

variable "app_domain_name" {
  description = "Optional DNS name for the app ALB, such as scrum.example.com."
  type        = string
  default     = ""
}

variable "livekit_domain_name" {
  description = "Optional DNS name for the self-hosted LiveKit NLB, such as livekit.example.com."
  type        = string
  default     = ""
}

variable "app_certificate_arn" {
  description = "Optional ACM certificate ARN for HTTPS on the app ALB."
  type        = string
  default     = ""
}

variable "livekit_certificate_arn" {
  description = "Optional ACM certificate ARN for TLS on the LiveKit NLB."
  type        = string
  default     = ""
}

variable "livekit_turn_certificate_arn" {
  description = "Optional ACM certificate ARN for TURN/TLS on the LiveKit NLB. The certificate must match livekit_turn_domain_name."
  type        = string
  default     = ""
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
  description = "Secrets Manager ARN containing LIVEKIT_API_KEY."
  type        = string

  validation {
    condition     = var.livekit_api_key_secret_arn != ""
    error_message = "livekit_api_key_secret_arn is required."
  }
}

variable "livekit_api_secret_secret_arn" {
  description = "Secrets Manager ARN containing LIVEKIT_API_SECRET."
  type        = string

  validation {
    condition     = var.livekit_api_secret_secret_arn != ""
    error_message = "livekit_api_secret_secret_arn is required."
  }
}

variable "livekit_config_secret_arn" {
  description = "Deprecated optional Secrets Manager ARN for a custom LIVEKIT_CONFIG override. The module now builds non-secret LiveKit config from Terraform inputs."
  type        = string
  default     = ""
}

variable "livekit_keys_secret_arn" {
  description = "Secrets Manager ARN containing LIVEKIT_KEYS in '<api-key>: <api-secret>' format for self-hosted LiveKit."
  type        = string
  default     = ""
}

variable "livekit_redis_engine_version" {
  description = "Pinned ElastiCache Redis engine version for LiveKit distributed routing."
  type        = string
  default     = "7.1"
}

variable "livekit_redis_node_type" {
  description = "ElastiCache node type for LiveKit Redis."
  type        = string
  default     = "cache.t4g.micro"
}

variable "livekit_redis_node_count" {
  description = "Number of ElastiCache nodes for LiveKit Redis. Use at least 2 for automatic failover."
  type        = number
  default     = 2
}

variable "fck_nat_ami_id" {
  description = "Pinned fck-nat AMI ID for us-east-1. Required for full deploys so the NAT instance never follows a latest AMI lookup."
  type        = string
  default     = ""
}

variable "fck_nat_instance_type" {
  description = "Instance type for the fck-nat NAT instance."
  type        = string
  default     = "t4g.micro"
}

variable "waf_rate_limit" {
  description = "Per-IP five-minute request rate limit enforced by AWS WAF on the app ALB."
  type        = number
  default     = 2000
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

variable "bedrock_model_arns" {
  description = "Bedrock model ARNs the app task may invoke."
  type        = list(string)
  default     = ["arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-sonic-v1:0"]

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
