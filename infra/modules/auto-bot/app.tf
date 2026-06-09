# App tier: ECR, ECS Fargate (ARM64, scale-to-zero), ALB + WAF, EFS-mounted
# board store. LiveKit runs on LiveKit Cloud, so there is no media plane here.

locals {
  app_container_name = "app"
  app_port           = 3000
  app_image          = var.app_image != "" ? var.app_image : "${aws_ecr_repository.app.repository_url}:bootstrap"

  # LiveKit Cloud only. LIVEKIT_URL/keys are injected as runtime config/secrets;
  # there is no self-hosted media plane in this module.
  livekit_public_url = var.livekit_cloud_url

  app_environment = compact([
    "VOICE_PROVIDER=${var.voice_provider}",
    "APP_ENV=production",
    "APP_AUTH_MODE=token",
    "APP_ROOM_ID=${var.app_room_id}",
    "APP_BOARD_ID=${var.app_board_id}",
    "BOARD_SQLITE_PATH=/srv/data/board.sqlite",
    "AWS_REGION=${var.aws_region}",
    "OPENAI_REALTIME_MODEL=${var.openai_realtime_model}",
    "OPENAI_REALTIME_TRANSCRIPTION_MODEL=${var.openai_realtime_transcription_model}",
    "OPENAI_REALTIME_TRANSLATION_MODEL=${var.openai_realtime_translation_model}",
    "OPENAI_REALTIME_TRANSLATION_TARGET_LANGUAGE=${var.openai_realtime_translation_target_language}",
    "NOVA_SONIC_MODEL=${var.nova_sonic_model}",
    "NOVA_SONIC_VOICE=${var.nova_sonic_voice}",
    "AGENT_PM_MODEL=${var.agent_pm_model}",
    "AGENT_REVIEW_MODEL=${var.agent_review_model}",
    "LIVEKIT_URL=${local.livekit_public_url}",
    "LIVEKIT_BROWSER_URL=${local.livekit_public_url}",
    "TRUST_PROXY_HEADERS=1",
    local.auth_enabled ? "APP_ALB_OIDC_AUTH=1" : "",
    local.auth_enabled ? "APP_ALB_ARN=${aws_lb.app.arn}" : "",
    local.auth_enabled ? "COGNITO_LOGOUT_URL=https://${aws_cognito_user_pool_domain.this[0].domain}.auth.${var.aws_region}.amazoncognito.com/logout?client_id=${aws_cognito_user_pool_client.this[0].id}&logout_uri=https://${var.auth_domain_name}/" : "",
    var.verbose_logging ? "PION_LOG_INFO=all" : "",
    var.host_emails != "" ? "HOST_EMAILS=${var.host_emails}" : "",
    var.allowed_emails != "" ? "ALLOWED_EMAILS=${var.allowed_emails}" : "",
    var.allowed_email_domains != "" ? "ALLOWED_EMAIL_DOMAINS=${var.allowed_email_domains}" : "",
    local.auth_enabled ? "APP_BASE_URL=https://${var.auth_domain_name}" : "",
    var.github_default_repo != "" ? "GITHUB_DEFAULT_REPO=${var.github_default_repo}" : "",
    var.github_allowed_repos != "" ? "GITHUB_ALLOWED_REPOS=${var.github_allowed_repos}" : "",
    "GITHUB_PR_COMMENTS_ENABLED=${var.github_pr_comments_enabled}",
    var.audit_log_path != "" ? "AUDIT_LOG_PATH=${var.audit_log_path}" : "",
    (!local.auth_enabled && var.app_base_url != "") ? "APP_BASE_URL=${var.app_base_url}" : "",
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

  task_execution_secret_arns = distinct(local.app_secret_arns)

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
}

resource "aws_ecr_repository" "app" {
  name                 = var.name_prefix
  image_tag_mutability = "IMMUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "terraform_data" "livekit_cloud_url_set" {
  input = var.livekit_cloud_url

  lifecycle {
    precondition {
      condition     = var.livekit_cloud_url != ""
      error_message = "livekit_cloud_url is required (LiveKit Cloud mode)."
    }
  }
}

resource "aws_lb" "app" {
  name               = "${var.name_prefix}-app"
  load_balancer_type = "application"
  security_groups    = [aws_security_group.app_alb.id]
  subnets            = local.public_subnet_ids

  # The app holds a long-lived browser WebSocket (/websocket) for board sync,
  # chat, and transcription. The ALB default 60s idle timeout tears idle WS
  # connections down ("Board connection interrupted. Reconnecting..."). Raise it.
  idle_timeout = 4000

  dynamic "access_logs" {
    for_each = var.enable_alb_access_logs ? [1] : []
    content {
      bucket  = aws_s3_bucket.alb_logs[0].id
      enabled = true
    }
  }
}

resource "aws_lb_target_group" "app" {
  name        = "${var.name_prefix}-app"
  port        = local.app_port
  protocol    = "HTTP"
  target_type = "ip"
  vpc_id      = local.vpc_id

  health_check {
    enabled             = true
    path                = "/healthz"
    matcher             = "200"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }
}

locals {
  # HTTPS edge is on whenever we have a cert: either the Cognito auth domain
  # (auth.tf provisions the ACM cert) or an externally supplied app_certificate_arn.
  https_enabled = local.auth_enabled || var.app_certificate_arn != ""
  # Prefer the validation resource's cert ARN when waiting is enabled, so the
  # HTTPS listener is ordered after validation (an ALB listener cannot attach a
  # cert still in PENDING_VALIDATION). Falls back to the raw cert ARN.
  app_https_cert_arn = (
    local.auth_enabled ?
    (var.acm_wait_for_validation ? aws_acm_certificate_validation.app[0].certificate_arn : aws_acm_certificate.app[0].arn) :
    var.app_certificate_arn
  )
}

# Plain HTTP forward — only when there is no HTTPS edge at all (dev/no-domain).
resource "aws_lb_listener" "app_http_forward" {
  count = local.https_enabled ? 0 : 1

  load_balancer_arn = aws_lb.app.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }
}

# With an HTTPS edge, port 80 redirects to 443.
resource "aws_lb_listener" "app_http_redirect" {
  count = local.https_enabled ? 1 : 0

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

# HTTPS listener. When Cognito auth is enabled, the DEFAULT action authenticates
# via Cognito (Google federation) before forwarding to the app — so every
# browser request is logged in at the edge. Webhook paths are exempted by
# higher-priority listener rules below.
resource "aws_lb_listener" "app_https" {
  count = local.https_enabled ? 1 : 0

  load_balancer_arn = aws_lb.app.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = local.app_https_cert_arn

  dynamic "default_action" {
    for_each = local.auth_enabled ? [1] : []
    content {
      type  = "authenticate-cognito"
      order = 1

      authenticate_cognito {
        user_pool_arn       = aws_cognito_user_pool.this[0].arn
        user_pool_client_id = aws_cognito_user_pool_client.this[0].id
        user_pool_domain    = aws_cognito_user_pool_domain.this[0].domain
        # Request email so the ALB-injected X-Amzn-Oidc-Data carries the email
        # claim the app keys access/host roles on. "openid" alone yields only sub.
        scope = "openid email profile"
        # Drop unauthenticated browser requests into the login flow.
        on_unauthenticated_request = "authenticate"
        session_timeout            = 43200
      }
    }
  }

  # The forward step. With auth enabled it runs as order 2 after the cognito
  # action; without auth it is the sole default action.
  dynamic "default_action" {
    for_each = local.auth_enabled ? [1] : []
    content {
      type             = "forward"
      target_group_arn = aws_lb_target_group.app.arn
      order            = 2
    }
  }

  dynamic "default_action" {
    for_each = local.auth_enabled ? [] : [1]
    content {
      type             = "forward"
      target_group_arn = aws_lb_target_group.app.arn
    }
  }
}

# Cognito-auth bypass paths. /healthz always bypasses (the ALB health check
# cannot do OIDC and it exposes no data). /jira/webhook is exempted ONLY when a
# webhook secret is configured, so the path is never opened to the internet
# without the in-app HMAC validation that is then its sole gate (fail-closed:
# the handler also 404s when the secret is unset).
locals {
  auth_bypass_paths = compact([
    "/healthz",
    var.jira_webhook_secret_secret_arn != "" ? "/jira/webhook" : "",
  ])
}

resource "aws_lb_listener_rule" "webhook_bypass" {
  count = local.auth_enabled ? 1 : 0

  listener_arn = aws_lb_listener.app_https[0].arn
  priority     = 10

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }

  condition {
    path_pattern {
      values = local.auth_bypass_paths
    }
  }
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

resource "aws_ecs_cluster" "this" {
  name = var.name_prefix

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

resource "aws_ecs_task_definition" "app" {
  family                   = "${var.name_prefix}-app"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.app_cpu
  memory                   = var.app_memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.app_task.arn

  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }

  # The task definition must reference a real pushed image. The initial bootstrap
  # uses `terragrunt apply -target=aws_ecr_repository.app`, which never plans this
  # resource, so this guard only fires on a full apply with APP_IMAGE unset.
  lifecycle {
    precondition {
      condition     = var.app_image != ""
      error_message = "app_image must be set to an immutable pushed image (run scripts/aws-app.sh deploy, which builds, signs, and sets APP_IMAGE before applying)."
    }
  }

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

resource "aws_ecs_service" "app" {
  name            = "${var.name_prefix}-app"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = var.app_desired_count
  launch_type     = "FARGATE"

  # The fast-iteration helper (scripts/aws-app.sh) flips desired_count via the
  # AWS API for scale-to-zero, so Terraform should not fight that drift.
  lifecycle {
    ignore_changes = [desired_count]
  }

  network_configuration {
    subnets          = local.public_subnet_ids
    security_groups  = [aws_security_group.app_task.id]
    assign_public_ip = true
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
    aws_efs_file_system_policy.board,
    aws_efs_mount_target.board,
  ]
}
