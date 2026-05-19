locals {
  az_count               = 2
  app_container_name     = "app"
  livekit_container_name = "livekit"
  app_port               = 3000
  app_image              = var.app_image != "" ? var.app_image : "${aws_ecr_repository.app.repository_url}:bootstrap"
  self_hosted_livekit    = var.livekit_deployment_mode == "self-hosted"
  livekit_public_signal_port = var.livekit_public_signal_port != 0 ? var.livekit_public_signal_port : (
    var.livekit_certificate_arn != "" ? 443 : var.livekit_signal_port
  )
  livekit_public_signal_port_suffix = local.livekit_public_signal_port == 443 ? "" : ":${local.livekit_public_signal_port}"
  livekit_self_hosted_url = var.livekit_url_override != "" ? var.livekit_url_override : (
    var.livekit_domain_name != "" ? "${var.livekit_certificate_arn != "" ? "wss" : "ws"}://${var.livekit_domain_name}${local.livekit_public_signal_port_suffix}" : "${var.livekit_certificate_arn != "" ? "wss" : "ws"}://${try(aws_lb.livekit[0].dns_name, "")}:${local.livekit_public_signal_port}"
  )
  livekit_public_url   = local.self_hosted_livekit ? local.livekit_self_hosted_url : var.livekit_cloud_url
  private_subnet_cidrs = [for subnet in aws_subnet.private : subnet.cidr_block]

  interface_endpoint_services = toset([
    "bedrock-runtime",
    "ecr.api",
    "ecr.dkr",
    "logs",
    "secretsmanager",
  ])

  app_environment = compact([
    "VOICE_PROVIDER=${var.voice_provider}",
    "APP_ENV=production",
    "APP_AUTH_MODE=token",
    "APP_ROOM_ID=${var.app_room_id}",
    "APP_BOARD_ID=${var.app_board_id}",
    "BOARD_SQLITE_PATH=/srv/data/board.sqlite",
    "AWS_REGION=${var.aws_region}",
    "OPENAI_REALTIME_MODEL=${var.openai_realtime_model}",
    "NOVA_SONIC_MODEL=${var.nova_sonic_model}",
    "NOVA_SONIC_VOICE=${var.nova_sonic_voice}",
    "AGENT_PM_MODEL=${var.agent_pm_model}",
    "AGENT_REVIEW_MODEL=${var.agent_review_model}",
    "LIVEKIT_URL=${local.livekit_public_url}",
    "LIVEKIT_BROWSER_URL=${local.livekit_public_url}",
    "TRUST_PROXY_HEADERS=1",
    var.github_default_repo != "" ? "GITHUB_DEFAULT_REPO=${var.github_default_repo}" : "",
    var.github_allowed_repos != "" ? "GITHUB_ALLOWED_REPOS=${var.github_allowed_repos}" : "",
    "GITHUB_PR_COMMENTS_ENABLED=${var.github_pr_comments_enabled}",
    var.audit_log_path != "" ? "AUDIT_LOG_PATH=${var.audit_log_path}" : "",
    var.app_base_url != "" ? "APP_BASE_URL=${var.app_base_url}" : "",
  ])

  app_secret_arns = compact([
    var.app_api_token_secret_arn,
    var.livekit_api_key_secret_arn,
    var.livekit_api_secret_secret_arn,
    var.openai_api_key_secret_arn,
    var.jira_api_token_secret_arn,
    var.jira_config_json_secret_arn,
    var.jira_webhook_secret_secret_arn,
    var.github_app_id_secret_arn,
    var.github_app_installation_id_secret_arn,
    var.github_app_private_key_secret_arn,
  ])

  task_execution_secret_arns = distinct(compact(concat(
    local.app_secret_arns,
    local.self_hosted_livekit ? [var.livekit_keys_secret_arn, var.livekit_config_secret_arn] : []
  )))

  bedrock_inference_profile_arns = [
    for model_id in distinct([var.agent_pm_model, var.agent_review_model]) :
    "arn:aws:bedrock:${var.aws_region}:${data.aws_caller_identity.current.account_id}:inference-profile/${model_id}"
    if startswith(model_id, "us.") || startswith(model_id, "eu.") || startswith(model_id, "apac.") || startswith(model_id, "global.")
  ]

  bedrock_policy_resource_arns = distinct(concat(var.bedrock_model_arns, local.bedrock_inference_profile_arns))

  app_secrets = concat(
    [
      {
        name      = "APP_API_TOKEN"
        valueFrom = var.app_api_token_secret_arn
      },
      {
        name      = "LIVEKIT_API_KEY"
        valueFrom = var.livekit_api_key_secret_arn
      },
      {
        name      = "LIVEKIT_API_SECRET"
        valueFrom = var.livekit_api_secret_secret_arn
      }
    ],
    var.openai_api_key_secret_arn == "" ? [] : [
      {
        name      = "OPENAI_API_KEY"
        valueFrom = var.openai_api_key_secret_arn
      }
    ],
    var.jira_api_token_secret_arn == "" ? [] : [
      {
        name      = "JIRA_API_TOKEN"
        valueFrom = var.jira_api_token_secret_arn
      }
    ],
    var.jira_config_json_secret_arn == "" ? [] : [
      {
        name      = "JIRA_CONFIG_JSON"
        valueFrom = var.jira_config_json_secret_arn
      }
    ],
    var.jira_webhook_secret_secret_arn == "" ? [] : [
      {
        name      = "JIRA_WEBHOOK_SECRET"
        valueFrom = var.jira_webhook_secret_secret_arn
      }
    ],
    var.github_app_id_secret_arn == "" ? [] : [
      {
        name      = "GITHUB_APP_ID"
        valueFrom = var.github_app_id_secret_arn
      }
    ],
    var.github_app_installation_id_secret_arn == "" ? [] : [
      {
        name      = "GITHUB_APP_INSTALLATION_ID"
        valueFrom = var.github_app_installation_id_secret_arn
      }
    ],
    var.github_app_private_key_secret_arn == "" ? [] : [
      {
        name      = "GITHUB_APP_PRIVATE_KEY"
        valueFrom = var.github_app_private_key_secret_arn
      }
    ]
  )

  livekit_config = <<-EOT
port: ${var.livekit_signal_port}
bind_addresses:
  - ""
rtc:
  tcp_port: ${var.livekit_tcp_port}
  udp_port: ${var.livekit_udp_port}
  use_external_ip: true
  congestion_control:
    enabled: true
    allow_pause: true
  allow_tcp_fallback: true
redis:
  address: ${try(aws_elasticache_replication_group.livekit[0].primary_endpoint_address, "redis.invalid")}:6379
  tls:
    enabled: true
prometheus_port: ${var.livekit_prometheus_port}
logging:
  json: true
turn:
  enabled: ${var.livekit_turn_enabled}
  udp_port: ${var.livekit_turn_udp_port}
  tls_port: ${var.livekit_turn_tls_port}
  external_tls: ${var.livekit_turn_tls_enabled}
  domain: ${var.livekit_turn_domain_name}
EOT

  livekit_environment = var.livekit_config_secret_arn == "" ? [
    {
      name  = "LIVEKIT_CONFIG"
      value = local.livekit_config
    }
  ] : []

  livekit_secrets = concat(
    [
      {
        name      = "LIVEKIT_KEYS"
        valueFrom = var.livekit_keys_secret_arn
      }
    ],
    var.livekit_config_secret_arn == "" ? [] : [
      {
        name      = "LIVEKIT_CONFIG"
        valueFrom = var.livekit_config_secret_arn
      }
    ]
  )

  livekit_port_mappings = concat([
    {
      containerPort = var.livekit_signal_port
      hostPort      = var.livekit_signal_port
      protocol      = "tcp"
    },
    {
      containerPort = var.livekit_tcp_port
      hostPort      = var.livekit_tcp_port
      protocol      = "tcp"
    },
    {
      containerPort = var.livekit_udp_port
      hostPort      = var.livekit_udp_port
      protocol      = "udp"
    }
    ],
    var.livekit_turn_enabled && var.livekit_turn_udp_enabled ? [
      {
        containerPort = var.livekit_turn_udp_port
        hostPort      = var.livekit_turn_udp_port
        protocol      = "udp"
      }
    ] : [],
    var.livekit_turn_enabled && var.livekit_turn_tls_enabled ? [
      {
        containerPort = var.livekit_turn_tls_port
        hostPort      = var.livekit_turn_tls_port
        protocol      = "tcp"
      }
    ] : [],
    [
      {
        containerPort = var.livekit_prometheus_port
        hostPort      = var.livekit_prometheus_port
        protocol      = "tcp"
      }
    ]
  )
}

data "aws_caller_identity" "current" {}

data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_ecr_repository" "app" {
  name                 = var.name_prefix
  image_tag_mutability = "IMMUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/${var.name_prefix}/app"
  retention_in_days = var.log_retention_days
}

resource "aws_cloudwatch_log_group" "livekit" {
  name              = "/ecs/${var.name_prefix}/livekit"
  retention_in_days = var.log_retention_days
}

resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
}

resource "aws_subnet" "public" {
  count = local.az_count

  vpc_id                  = aws_vpc.this.id
  availability_zone       = data.aws_availability_zones.available.names[count.index]
  cidr_block              = cidrsubnet(aws_vpc.this.cidr_block, 8, count.index + 21)
  map_public_ip_on_launch = true
}

resource "aws_subnet" "private" {
  count = local.az_count

  vpc_id                  = aws_vpc.this.id
  availability_zone       = data.aws_availability_zones.available.names[count.index]
  cidr_block              = cidrsubnet(aws_vpc.this.cidr_block, 8, count.index + 121)
  map_public_ip_on_launch = false
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
}

resource "aws_route_table_association" "public" {
  count = local.az_count

  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table" "private" {
  count = local.az_count

  vpc_id = aws_vpc.this.id
}

resource "aws_route_table_association" "private" {
  count = local.az_count

  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[count.index].id
}

resource "terraform_data" "fck_nat_ami_pin" {
  input = var.fck_nat_ami_id

  lifecycle {
    precondition {
      condition     = can(regex("^ami-[0-9a-f]{8,17}$", var.fck_nat_ami_id))
      error_message = "fck_nat_ami_id must be set to an explicit us-east-1 fck-nat AMI ID. Do not use the module's latest AMI lookup."
    }
  }
}

resource "terraform_data" "app_image_pin" {
  input = var.app_image

  lifecycle {
    precondition {
      condition     = var.app_image != ""
      error_message = "app_image must be set to an immutable pushed image before full deploy. Use the initial -target=aws_ecr_repository.app bootstrap only to create ECR."
    }
  }
}

resource "terraform_data" "livekit_inputs" {
  input = var.livekit_deployment_mode

  lifecycle {
    precondition {
      condition     = local.self_hosted_livekit || var.livekit_cloud_url != ""
      error_message = "livekit_cloud_url must be set when livekit_deployment_mode is cloud."
    }

    precondition {
      condition     = !local.self_hosted_livekit || var.livekit_keys_secret_arn != ""
      error_message = "livekit_keys_secret_arn is required for self-hosted LiveKit."
    }

    precondition {
      condition     = !local.self_hosted_livekit || !var.livekit_turn_tls_enabled || (var.livekit_turn_certificate_arn != "" && var.livekit_turn_domain_name != "")
      error_message = "TURN/TLS requires livekit_turn_certificate_arn and livekit_turn_domain_name."
    }
  }
}

module "fck_nat" {
  source  = "RaJiska/fck-nat/aws"
  version = "1.4.0"

  name      = "${var.name_prefix}-fck-nat"
  vpc_id    = aws_vpc.this.id
  subnet_id = aws_subnet.public[0].id

  ami_id               = var.fck_nat_ami_id
  instance_type        = var.fck_nat_instance_type
  ebs_root_volume_size = 8
  encryption           = true
  ha_mode              = true
  use_spot_instances   = false
  use_ssh              = false
  attach_ssm_policy    = false
  use_cloudwatch_agent = true

  update_route_tables = true
  route_tables_ids = {
    for idx, route_table in aws_route_table.private : "private-${idx}" => route_table.id
  }

  depends_on = [terraform_data.fck_nat_ami_pin]

  tags = {
    Name = "${var.name_prefix}-fck-nat"
  }
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.aws_region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = aws_route_table.private[*].id
}

resource "aws_vpc_endpoint" "interface" {
  for_each = local.interface_endpoint_services

  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.aws_region}.${each.key}"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = aws_subnet.private[*].id
  security_group_ids  = [aws_security_group.vpc_endpoint.id]
  private_dns_enabled = true
}

resource "aws_security_group" "app_alb" {
  name        = "${var.name_prefix}-app-alb"
  description = "Public HTTP/HTTPS ingress to the app ALB"
  vpc_id      = aws_vpc.this.id

  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = var.allowed_ingress_cidrs
  }

  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = var.allowed_ingress_cidrs
  }

  egress {
    description = "App target traffic"
    from_port   = local.app_port
    to_port     = local.app_port
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
  }
}

resource "aws_security_group" "app_task" {
  name        = "${var.name_prefix}-app-task"
  description = "App task ingress from ALB"
  vpc_id      = aws_vpc.this.id

  ingress {
    description     = "App HTTP from ALB"
    from_port       = local.app_port
    to_port         = local.app_port
    protocol        = "tcp"
    security_groups = [aws_security_group.app_alb.id]
  }

  egress {
    description = "HTTPS to AWS endpoints and external APIs through fck-nat"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "LiveKit signaling through the public NLB"
    from_port   = var.livekit_signal_port
    to_port     = var.livekit_signal_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "LiveKit TCP RTC fallback through the public NLB"
    from_port   = var.livekit_tcp_port
    to_port     = var.livekit_tcp_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "LiveKit UDP RTC media through the public NLB"
    from_port   = var.livekit_udp_port
    to_port     = var.livekit_udp_port
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "EFS board store"
    from_port   = 2049
    to_port     = 2049
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
  }
}

resource "aws_security_group" "efs" {
  name        = "${var.name_prefix}-efs"
  description = "Board persistence EFS ingress from app tasks"
  vpc_id      = aws_vpc.this.id

  ingress {
    description     = "NFS from app task"
    from_port       = 2049
    to_port         = 2049
    protocol        = "tcp"
    security_groups = [aws_security_group.app_task.id]
  }
}

resource "aws_security_group" "livekit" {
  count = local.self_hosted_livekit ? 1 : 0

  name        = "${var.name_prefix}-livekit"
  description = "LiveKit signaling and RTC ingress"
  vpc_id      = aws_vpc.this.id

  ingress {
    description = "LiveKit signaling"
    from_port   = var.livekit_signal_port
    to_port     = var.livekit_signal_port
    protocol    = "tcp"
    cidr_blocks = var.allowed_ingress_cidrs
  }

  ingress {
    description = "NLB health checks from VPC"
    from_port   = var.livekit_signal_port
    to_port     = var.livekit_signal_port
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
  }

  ingress {
    description = "LiveKit TCP RTC fallback"
    from_port   = var.livekit_tcp_port
    to_port     = var.livekit_tcp_port
    protocol    = "tcp"
    cidr_blocks = var.allowed_ingress_cidrs
  }

  ingress {
    description = "LiveKit UDP RTC media"
    from_port   = var.livekit_udp_port
    to_port     = var.livekit_udp_port
    protocol    = "udp"
    cidr_blocks = var.allowed_ingress_cidrs
  }

  dynamic "ingress" {
    for_each = var.livekit_turn_enabled && var.livekit_turn_udp_enabled ? [1] : []

    content {
      description = "LiveKit embedded TURN UDP"
      from_port   = var.livekit_turn_udp_port
      to_port     = var.livekit_turn_udp_port
      protocol    = "udp"
      cidr_blocks = var.allowed_ingress_cidrs
    }
  }

  dynamic "ingress" {
    for_each = var.livekit_turn_enabled && var.livekit_turn_tls_enabled ? [1] : []

    content {
      description = "LiveKit embedded TURN TLS"
      from_port   = var.livekit_turn_tls_port
      to_port     = var.livekit_turn_tls_port
      protocol    = "tcp"
      cidr_blocks = var.allowed_ingress_cidrs
    }
  }

  ingress {
    description = "Prometheus metrics from VPC"
    from_port   = var.livekit_prometheus_port
    to_port     = var.livekit_prometheus_port
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
  }

  egress {
    description = "HTTPS to AWS endpoints"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "LiveKit TCP RTC fallback to participants"
    from_port   = var.livekit_tcp_port
    to_port     = var.livekit_tcp_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "LiveKit UDP RTC media to participants"
    from_port   = var.livekit_udp_port
    to_port     = var.livekit_udp_port
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  dynamic "egress" {
    for_each = var.livekit_turn_enabled && var.livekit_turn_udp_enabled ? [1] : []

    content {
      description = "TURN UDP relay to participants"
      from_port   = var.livekit_turn_udp_port
      to_port     = var.livekit_turn_udp_port
      protocol    = "udp"
      cidr_blocks = ["0.0.0.0/0"]
    }
  }

  dynamic "egress" {
    for_each = var.livekit_turn_enabled && var.livekit_turn_tls_enabled ? [1] : []

    content {
      description = "TURN TLS relay to participants"
      from_port   = var.livekit_turn_tls_port
      to_port     = var.livekit_turn_tls_port
      protocol    = "tcp"
      cidr_blocks = ["0.0.0.0/0"]
    }
  }
}

resource "aws_security_group" "livekit_redis" {
  count = local.self_hosted_livekit ? 1 : 0

  name        = "${var.name_prefix}-livekit-redis"
  description = "ElastiCache Redis ingress from LiveKit tasks"
  vpc_id      = aws_vpc.this.id

  ingress {
    description     = "Redis TLS from LiveKit"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.livekit[0].id]
  }
}

resource "aws_security_group" "vpc_endpoint" {
  name        = "${var.name_prefix}-vpc-endpoint"
  description = "Private interface endpoint ingress from ECS tasks"
  vpc_id      = aws_vpc.this.id

  ingress {
    description = "HTTPS from private subnets"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = local.private_subnet_cidrs
  }
}

resource "aws_elasticache_subnet_group" "livekit" {
  count = local.self_hosted_livekit ? 1 : 0

  name       = "${var.name_prefix}-livekit"
  subnet_ids = aws_subnet.private[*].id
}

resource "aws_elasticache_replication_group" "livekit" {
  count = local.self_hosted_livekit ? 1 : 0

  replication_group_id       = "${var.name_prefix}-livekit"
  description                = "Redis routing and message bus for LiveKit"
  engine                     = "redis"
  engine_version             = var.livekit_redis_engine_version
  node_type                  = var.livekit_redis_node_type
  num_cache_clusters         = var.livekit_redis_node_count
  automatic_failover_enabled = var.livekit_redis_node_count > 1
  multi_az_enabled           = var.livekit_redis_node_count > 1
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  subnet_group_name          = aws_elasticache_subnet_group.livekit[0].name
  security_group_ids         = [aws_security_group.livekit_redis[0].id]
  apply_immediately          = true
}

resource "aws_lb" "app" {
  name               = "${var.name_prefix}-app"
  load_balancer_type = "application"
  security_groups    = [aws_security_group.app_alb.id]
  subnets            = aws_subnet.public[*].id
}

resource "aws_wafv2_web_acl" "app" {
  name  = "${var.name_prefix}-app"
  scope = "REGIONAL"

  default_action {
    allow {}
  }

  rule {
    name     = "AWSManagedRulesAmazonIpReputationList"
    priority = 10

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesAmazonIpReputationList"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.name_prefix}-ip-reputation"
      sampled_requests_enabled   = true
    }
  }

  rule {
    name     = "AWSManagedRulesCommonRuleSet"
    priority = 20

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesCommonRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.name_prefix}-common"
      sampled_requests_enabled   = true
    }
  }

  rule {
    name     = "AWSManagedRulesKnownBadInputsRuleSet"
    priority = 30

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesKnownBadInputsRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.name_prefix}-known-bad-inputs"
      sampled_requests_enabled   = true
    }
  }

  rule {
    name     = "RateLimit"
    priority = 40

    action {
      block {}
    }

    statement {
      rate_based_statement {
        aggregate_key_type = "IP"
        limit              = var.waf_rate_limit
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.name_prefix}-rate-limit"
      sampled_requests_enabled   = true
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "${var.name_prefix}-app"
    sampled_requests_enabled   = true
  }
}

resource "aws_wafv2_web_acl_association" "app" {
  resource_arn = aws_lb.app.arn
  web_acl_arn  = aws_wafv2_web_acl.app.arn
}

resource "aws_route53_record" "app" {
  count = var.hosted_zone_id != "" && var.app_domain_name != "" ? 1 : 0

  zone_id = var.hosted_zone_id
  name    = var.app_domain_name
  type    = "A"

  alias {
    name                   = aws_lb.app.dns_name
    zone_id                = aws_lb.app.zone_id
    evaluate_target_health = true
  }
}

resource "aws_lb_target_group" "app" {
  name        = "${var.name_prefix}-app"
  port        = local.app_port
  protocol    = "HTTP"
  target_type = "ip"
  vpc_id      = aws_vpc.this.id

  health_check {
    enabled             = true
    path                = "/"
    matcher             = "200"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }
}

resource "aws_lb_listener" "app_http_forward" {
  count = var.app_certificate_arn == "" ? 1 : 0

  load_balancer_arn = aws_lb.app.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }
}

resource "aws_lb_listener" "app_http_redirect" {
  count = var.app_certificate_arn == "" ? 0 : 1

  load_balancer_arn = aws_lb.app.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"

    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener" "app_https" {
  count = var.app_certificate_arn == "" ? 0 : 1

  load_balancer_arn = aws_lb.app.arn
  port              = 443
  protocol          = "HTTPS"
  certificate_arn   = var.app_certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }
}

resource "aws_lb" "livekit" {
  count = local.self_hosted_livekit ? 1 : 0

  name               = "${var.name_prefix}-livekit"
  load_balancer_type = "network"
  subnets            = aws_subnet.public[*].id
}

resource "aws_lb_target_group" "livekit_signal" {
  count = local.self_hosted_livekit ? 1 : 0

  name        = "${var.name_prefix}-lk-signal"
  port        = var.livekit_signal_port
  protocol    = "TCP"
  target_type = "ip"
  vpc_id      = aws_vpc.this.id

  health_check {
    enabled             = true
    protocol            = "TCP"
    port                = tostring(var.livekit_signal_port)
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }
}

resource "aws_lb_target_group" "livekit_tcp" {
  count = local.self_hosted_livekit ? 1 : 0

  name        = "${var.name_prefix}-lk-tcp"
  port        = var.livekit_tcp_port
  protocol    = "TCP"
  target_type = "ip"
  vpc_id      = aws_vpc.this.id

  health_check {
    enabled             = true
    protocol            = "TCP"
    port                = tostring(var.livekit_signal_port)
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }
}

resource "aws_lb_target_group" "livekit_udp" {
  count = local.self_hosted_livekit ? 1 : 0

  name        = "${var.name_prefix}-lk-udp"
  port        = var.livekit_udp_port
  protocol    = "UDP"
  target_type = "ip"
  vpc_id      = aws_vpc.this.id

  health_check {
    enabled             = true
    protocol            = "TCP"
    port                = tostring(var.livekit_signal_port)
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }
}

resource "aws_lb_target_group" "livekit_turn_udp" {
  count = local.self_hosted_livekit && var.livekit_turn_enabled && var.livekit_turn_udp_enabled ? 1 : 0

  name        = "${var.name_prefix}-lk-turn-u"
  port        = var.livekit_turn_udp_port
  protocol    = "UDP"
  target_type = "ip"
  vpc_id      = aws_vpc.this.id

  health_check {
    enabled             = true
    protocol            = "TCP"
    port                = tostring(var.livekit_signal_port)
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }
}

resource "aws_lb_target_group" "livekit_turn_tls" {
  count = local.self_hosted_livekit && var.livekit_turn_enabled && var.livekit_turn_tls_enabled ? 1 : 0

  name        = "${var.name_prefix}-lk-turn-t"
  port        = var.livekit_turn_tls_port
  protocol    = "TCP"
  target_type = "ip"
  vpc_id      = aws_vpc.this.id

  health_check {
    enabled             = true
    protocol            = "TCP"
    port                = tostring(var.livekit_signal_port)
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }
}

resource "aws_lb_listener" "livekit_signal_tcp" {
  count = local.self_hosted_livekit && var.livekit_certificate_arn == "" ? 1 : 0

  load_balancer_arn = aws_lb.livekit[0].arn
  port              = local.livekit_public_signal_port
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_signal[0].arn
  }
}

resource "aws_lb_listener" "livekit_signal_tls" {
  count = local.self_hosted_livekit && var.livekit_certificate_arn != "" ? 1 : 0

  load_balancer_arn = aws_lb.livekit[0].arn
  port              = local.livekit_public_signal_port
  protocol          = "TLS"
  certificate_arn   = var.livekit_certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_signal[0].arn
  }
}

resource "aws_lb_listener" "livekit_tcp" {
  count = local.self_hosted_livekit ? 1 : 0

  load_balancer_arn = aws_lb.livekit[0].arn
  port              = var.livekit_tcp_port
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_tcp[0].arn
  }
}

resource "aws_lb_listener" "livekit_udp" {
  count = local.self_hosted_livekit ? 1 : 0

  load_balancer_arn = aws_lb.livekit[0].arn
  port              = var.livekit_udp_port
  protocol          = "UDP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_udp[0].arn
  }
}

resource "aws_lb_listener" "livekit_turn_udp" {
  count = local.self_hosted_livekit && var.livekit_turn_enabled && var.livekit_turn_udp_enabled ? 1 : 0

  load_balancer_arn = aws_lb.livekit[0].arn
  port              = var.livekit_turn_udp_port
  protocol          = "UDP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_turn_udp[0].arn
  }
}

resource "aws_lb_listener" "livekit_turn_tls" {
  count = local.self_hosted_livekit && var.livekit_turn_enabled && var.livekit_turn_tls_enabled ? 1 : 0

  load_balancer_arn = aws_lb.livekit[0].arn
  port              = var.livekit_turn_tls_port
  protocol          = "TLS"
  certificate_arn   = var.livekit_turn_certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_turn_tls[0].arn
  }
}

resource "aws_route53_record" "livekit" {
  count = local.self_hosted_livekit && var.hosted_zone_id != "" && var.livekit_domain_name != "" ? 1 : 0

  zone_id = var.hosted_zone_id
  name    = var.livekit_domain_name
  type    = "A"

  alias {
    name                   = aws_lb.livekit[0].dns_name
    zone_id                = aws_lb.livekit[0].zone_id
    evaluate_target_health = true
  }
}

resource "aws_route53_record" "livekit_turn" {
  count = local.self_hosted_livekit && var.hosted_zone_id != "" && var.livekit_turn_domain_name != "" ? 1 : 0

  zone_id = var.hosted_zone_id
  name    = var.livekit_turn_domain_name
  type    = "A"

  alias {
    name                   = aws_lb.livekit[0].dns_name
    zone_id                = aws_lb.livekit[0].zone_id
    evaluate_target_health = true
  }
}

resource "aws_efs_file_system" "board" {
  creation_token = "${var.name_prefix}-board"
  encrypted      = true

  lifecycle_policy {
    transition_to_ia = "AFTER_30_DAYS"
  }

  tags = {
    Name = "${var.name_prefix}-board"
  }
}

resource "aws_efs_access_point" "board" {
  file_system_id = aws_efs_file_system.board.id

  posix_user {
    uid = 10001
    gid = 10001
  }

  root_directory {
    path = "/auto-bot"

    creation_info {
      owner_uid   = 10001
      owner_gid   = 10001
      permissions = "0700"
    }
  }
}

resource "aws_efs_mount_target" "board" {
  count = local.az_count

  file_system_id  = aws_efs_file_system.board.id
  subnet_id       = aws_subnet.private[count.index].id
  security_groups = [aws_security_group.efs.id]
}

resource "aws_ecs_cluster" "this" {
  name = var.name_prefix

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

resource "aws_cloudwatch_dashboard" "ops" {
  dashboard_name = "${var.name_prefix}-ops"

  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          region = var.aws_region
          title  = "ECS CPU and memory"
          metrics = [
            ["AWS/ECS", "CPUUtilization", "ClusterName", aws_ecs_cluster.this.name, "ServiceName", aws_ecs_service.app.name],
            [".", "MemoryUtilization", ".", ".", ".", "."],
            ["AWS/ECS", "CPUUtilization", "ClusterName", aws_ecs_cluster.this.name, "ServiceName", try(aws_ecs_service.livekit[0].name, "")],
            [".", "MemoryUtilization", ".", ".", ".", "."],
          ]
          stat = "Average"
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          region = var.aws_region
          title  = "Load balancers and WAF"
          metrics = [
            ["AWS/ApplicationELB", "TargetResponseTime", "LoadBalancer", aws_lb.app.arn_suffix],
            ["AWS/ApplicationELB", "HTTPCode_Target_5XX_Count", "LoadBalancer", aws_lb.app.arn_suffix],
            ["AWS/NetworkELB", "HealthyHostCount", "LoadBalancer", try(aws_lb.livekit[0].arn_suffix, "")],
            ["AWS/WAFV2", "BlockedRequests", "WebACL", aws_wafv2_web_acl.app.name, "Region", var.aws_region, "Rule", "ALL"],
          ]
          stat = "Average"
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 6
        width  = 12
        height = 6
        properties = {
          region = var.aws_region
          title  = "LiveKit Redis"
          metrics = [
            ["AWS/ElastiCache", "CPUUtilization", "ReplicationGroupId", try(aws_elasticache_replication_group.livekit[0].replication_group_id, "")],
            [".", "CurrConnections", ".", "."],
            [".", "NetworkBytesIn", ".", "."],
            [".", "NetworkBytesOut", ".", "."],
          ]
          stat = "Average"
        }
      },
    ]
  })
}

resource "aws_iam_role" "task_execution" {
  name = "${var.name_prefix}-ecs-execution"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })
}

resource "aws_iam_role_policy" "task_execution" {
  name = "${var.name_prefix}-ecs-execution"
  role = aws_iam_role.task_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ECRAuthorization"
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      },
      {
        Sid    = "ECRPullAppImage"
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:BatchGetImage",
          "ecr:GetDownloadUrlForLayer",
        ]
        Resource = aws_ecr_repository.app.arn
      },
      {
        Sid    = "WriteContainerLogs"
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ]
        Resource = [
          "${aws_cloudwatch_log_group.app.arn}:*",
          "${aws_cloudwatch_log_group.livekit.arn}:*",
        ]
      },
      {
        Sid      = "ReadRuntimeSecrets"
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = local.task_execution_secret_arns
      }
    ]
  })
}

resource "aws_iam_role" "app_task" {
  name = "${var.name_prefix}-app-task"

  assume_role_policy = aws_iam_role.task_execution.assume_role_policy
}

resource "aws_iam_role_policy" "app_bedrock" {
  name = "${var.name_prefix}-bedrock"
  role = aws_iam_role.app_task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "bedrock:InvokeModel",
          "bedrock:InvokeModelWithBidirectionalStream",
          "bedrock:InvokeModelWithResponseStream",
        ]
        Resource = local.bedrock_policy_resource_arns
      }
    ]
  })
}

resource "aws_iam_role_policy" "app_efs" {
  name = "${var.name_prefix}-efs"
  role = aws_iam_role.app_task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "elasticfilesystem:ClientMount",
          "elasticfilesystem:ClientWrite",
        ]
        Resource = aws_efs_file_system.board.arn
        Condition = {
          StringEquals = {
            "elasticfilesystem:AccessPointArn" = aws_efs_access_point.board.arn
          }
        }
      }
    ]
  })
}

resource "aws_efs_file_system_policy" "board" {
  file_system_id = aws_efs_file_system.board.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyUnencryptedTransport"
        Effect    = "Deny"
        Principal = "*"
        Action    = "*"
        Resource  = aws_efs_file_system.board.arn
        Condition = {
          Bool = {
            "aws:SecureTransport" = "false"
          }
        }
      },
      {
        Sid    = "AllowAppTaskAccessPoint"
        Effect = "Allow"
        Principal = {
          AWS = aws_iam_role.app_task.arn
        }
        Action = [
          "elasticfilesystem:ClientMount",
          "elasticfilesystem:ClientWrite",
        ]
        Resource = aws_efs_file_system.board.arn
        Condition = {
          StringEquals = {
            "elasticfilesystem:AccessPointArn" = aws_efs_access_point.board.arn
          }
        }
      }
    ]
  })
}

resource "aws_ecs_task_definition" "app" {
  family                   = "${var.name_prefix}-app"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.app_cpu
  memory                   = var.app_memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.app_task.arn

  volume {
    name = "board-data"

    efs_volume_configuration {
      file_system_id     = aws_efs_file_system.board.id
      transit_encryption = "ENABLED"

      authorization_config {
        access_point_id = aws_efs_access_point.board.id
        iam             = "ENABLED"
      }
    }
  }

  container_definitions = jsonencode([
    {
      name      = local.app_container_name
      image     = local.app_image
      essential = true

      linuxParameters = {
        initProcessEnabled = true
      }

      portMappings = [
        {
          containerPort = local.app_port
          hostPort      = local.app_port
          protocol      = "tcp"
        }
      ]

      mountPoints = [
        {
          sourceVolume  = "board-data"
          containerPath = "/srv/data"
          readOnly      = false
        }
      ]

      environment = [
        for item in local.app_environment : {
          name  = split("=", item)[0]
          value = join("=", slice(split("=", item), 1, length(split("=", item))))
        }
      ]

      secrets = local.app_secrets

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = aws_cloudwatch_log_group.app.name
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "app"
        }
      }
    }
  ])
}

resource "aws_ecs_task_definition" "livekit" {
  count = local.self_hosted_livekit ? 1 : 0

  family                   = "${var.name_prefix}-livekit"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.livekit_cpu
  memory                   = var.livekit_memory
  execution_role_arn       = aws_iam_role.task_execution.arn

  container_definitions = jsonencode([
    {
      name      = local.livekit_container_name
      image     = var.livekit_image
      essential = true

      portMappings = local.livekit_port_mappings
      environment  = local.livekit_environment

      secrets = local.livekit_secrets

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = aws_cloudwatch_log_group.livekit.name
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "livekit"
        }
      }
    }
  ])
}

resource "aws_ecs_service" "livekit" {
  count = local.self_hosted_livekit ? 1 : 0

  name            = "${var.name_prefix}-livekit"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.livekit[0].arn
  desired_count   = var.livekit_desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.livekit[0].id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.livekit_signal[0].arn
    container_name   = local.livekit_container_name
    container_port   = var.livekit_signal_port
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.livekit_tcp[0].arn
    container_name   = local.livekit_container_name
    container_port   = var.livekit_tcp_port
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.livekit_udp[0].arn
    container_name   = local.livekit_container_name
    container_port   = var.livekit_udp_port
  }

  dynamic "load_balancer" {
    for_each = var.livekit_turn_enabled && var.livekit_turn_udp_enabled ? [1] : []

    content {
      target_group_arn = aws_lb_target_group.livekit_turn_udp[0].arn
      container_name   = local.livekit_container_name
      container_port   = var.livekit_turn_udp_port
    }
  }

  dynamic "load_balancer" {
    for_each = var.livekit_turn_enabled && var.livekit_turn_tls_enabled ? [1] : []

    content {
      target_group_arn = aws_lb_target_group.livekit_turn_tls[0].arn
      container_name   = local.livekit_container_name
      container_port   = var.livekit_turn_tls_port
    }
  }

  depends_on = [
    aws_iam_role_policy.task_execution,
    aws_lb_listener.livekit_signal_tcp,
    aws_lb_listener.livekit_signal_tls,
    aws_lb_listener.livekit_tcp,
    aws_lb_listener.livekit_udp,
    aws_lb_listener.livekit_turn_udp,
    aws_lb_listener.livekit_turn_tls,
    aws_elasticache_replication_group.livekit,
    aws_vpc_endpoint.interface,
    aws_vpc_endpoint.s3,
    terraform_data.livekit_inputs,
    module.fck_nat,
  ]
}

resource "aws_ecs_service" "app" {
  name            = "${var.name_prefix}-app"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = var.app_desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.app_task.id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = local.app_container_name
    container_port   = local.app_port
  }

  depends_on = [
    aws_iam_role_policy.task_execution,
    aws_iam_role_policy.app_bedrock,
    aws_iam_role_policy.app_efs,
    aws_lb_listener.app_http_forward,
    aws_lb_listener.app_http_redirect,
    aws_lb_listener.livekit_signal_tcp,
    aws_lb_listener.livekit_signal_tls,
    aws_vpc_endpoint.interface,
    aws_vpc_endpoint.s3,
    aws_efs_file_system_policy.board,
    aws_efs_mount_target.board,
    module.fck_nat,
    aws_ecs_service.livekit,
  ]
}
