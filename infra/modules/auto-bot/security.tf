# Security groups, WAF, KMS (cosign signing), and IAM.

resource "aws_security_group" "app_alb" {
  name        = "${var.name_prefix}-app-alb"
  description = "Public HTTP/HTTPS ingress to the app ALB"
  vpc_id      = local.vpc_id

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
    cidr_blocks = [local.vpc_cidr]
  }

  # The authenticate-cognito listener action makes a server-side call from the
  # ALB to the Cognito token/userinfo endpoints during the /oauth2/idpresponse
  # callback. Without this egress the auth action fails with an ELB 500 on the
  # callback leg (the authorize leg, a plain 302, is unaffected).
  egress {
    description = "ALB to Cognito token/userinfo endpoints (authenticate-cognito)"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# The app task gets a public IP for NAT-free egress over the existing IGW, but
# this security group only admits inbound from the ALB, so it is not reachable
# from the internet. Egress is limited to HTTPS (LiveKit Cloud, Bedrock, ECR,
# Secrets Manager, CloudWatch) and NFS to the board EFS mount.
resource "aws_security_group" "app_task" {
  name        = "${var.name_prefix}-app-task"
  description = "App task ingress from ALB only"
  vpc_id      = local.vpc_id

  ingress {
    description     = "App HTTP from ALB"
    from_port       = local.app_port
    to_port         = local.app_port
    protocol        = "tcp"
    security_groups = [aws_security_group.app_alb.id]
  }

  egress {
    description = "HTTPS to AWS endpoints, LiveKit Cloud signaling, and external APIs"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # The server-side Nova Sonic voice agent joins the LiveKit Cloud room over
  # WebRTC, whose media plane is UDP (ICE/RTP). Without UDP egress the agent
  # connects signaling (wss/443) but times out on media ("could not connect
  # after timeout"). Egress-only to the internet; nothing can reach the task
  # inbound (the task SG admits inbound only from the ALB).
  egress {
    description = "WebRTC/UDP media to LiveKit Cloud"
    from_port   = 0
    to_port     = 65535
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "EFS board store"
    from_port   = 2049
    to_port     = 2049
    protocol    = "tcp"
    cidr_blocks = [local.vpc_cidr]
  }
}

resource "aws_security_group" "efs" {
  name        = "${var.name_prefix}-efs"
  description = "Board persistence EFS ingress from app tasks"
  vpc_id      = local.vpc_id

  ingress {
    description     = "NFS from app task"
    from_port       = 2049
    to_port         = 2049
    protocol        = "tcp"
    security_groups = [aws_security_group.app_task.id]
  }
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

# Asymmetric KMS key used by cosign to sign and verify the app image. Signing
# and verification run non-interactively via awskms:// so the local fast-iteration
# loop never prompts for a browser-based OIDC identity.
resource "aws_kms_key" "cosign" {
  description              = "${var.name_prefix} cosign image signing key"
  customer_master_key_spec = "ECC_NIST_P256"
  key_usage                = "SIGN_VERIFY"
  enable_key_rotation      = false
  deletion_window_in_days  = 7
}

resource "aws_kms_alias" "cosign" {
  name          = "alias/${var.name_prefix}-cosign"
  target_key_id = aws_kms_key.cosign.key_id
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
        Resource = ["${aws_cloudwatch_log_group.app.arn}:*"]
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
