variable "aws_region" {
  description = "AWS region for ECS, load balancers, CloudWatch, and Bedrock runtime access."
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix used for AWS resource names."
  type        = string
}

variable "allowed_ingress_cidrs" {
  description = "CIDR blocks allowed to reach the public app and LiveKit endpoints."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "app_image" {
  description = "Fully qualified container image URI for the Go app. Leave empty to use this module's ECR repository with :latest."
  type        = string
  default     = ""
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
  default     = "livekit/livekit-server@sha256:100b9a870616d02f5e3795b34e0b593b5054a26f8131a94fd3fa322ed3154b16"
}

variable "livekit_desired_count" {
  description = "Number of LiveKit tasks. Keep this at 1 until Redis/multi-node routing is added."
  type        = number
  default     = 1
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

variable "livekit_url_override" {
  description = "Optional browser-facing LiveKit URL, such as wss://livekit.example.com. Defaults to ws://<nlb-dns>:7880."
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
  description = "Secrets Manager ARN containing the LiveKit YAML config body for LIVEKIT_CONFIG."
  type        = string

  validation {
    condition     = var.livekit_config_secret_arn != ""
    error_message = "livekit_config_secret_arn is required."
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

variable "bedrock_model_arns" {
  description = "Bedrock model ARNs the app task may invoke. Use * for early testing, then narrow."
  type        = list(string)
  default     = ["*"]
}

variable "log_retention_days" {
  description = "CloudWatch log retention in days."
  type        = number
  default     = 14
}
