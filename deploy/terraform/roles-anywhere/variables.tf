variable "name_prefix" {
  description = "Prefix for created resource names."
  type        = string
  default     = "auto-bot"
}

variable "ca_certificate_pem" {
  description = "PEM-encoded CA certificate that signs the pod's leaf cert (the trust anchor). Must have basicConstraints CA:TRUE."
  type        = string
}

variable "certificate_cn" {
  description = "Common Name (CN) of the leaf certificate the pod presents. The role trust policy is bound to this CN."
  type        = string
  default     = "auto-bot-pod"
}

variable "agent_model_arns" {
  description = <<-EOT
    ARNs the Claude agent models may invoke. For inference profiles (e.g.
    us.anthropic.claude-*), include BOTH the inference-profile ARN and the
    underlying foundation-model ARNs in every region the profile routes to.
  EOT
  type        = list(string)
  default     = []
}

variable "nova_sonic_model_arns" {
  description = "Foundation-model ARNs for Nova Sonic speech-to-speech (us-east-1 or us-west-2; NOT us-east-2)."
  type        = list(string)
  default = [
    "arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-2-sonic-v1:0",
    "arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-sonic-v1:0",
  ]
}
