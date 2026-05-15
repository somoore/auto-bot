locals {
  az_count               = 2
  app_container_name     = "app"
  livekit_container_name = "livekit"
  app_port               = 3000
  app_image              = var.app_image != "" ? var.app_image : "${aws_ecr_repository.app.repository_url}:latest"
  livekit_public_url     = var.livekit_url_override != "" ? var.livekit_url_override : "${var.livekit_certificate_arn != "" ? "wss" : "ws"}://${aws_lb.livekit.dns_name}:${var.livekit_signal_port}"

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
    "LIVEKIT_URL=${local.livekit_public_url}",
    "TRUST_PROXY_HEADERS=1",
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
  ])

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
    ]
  )

  livekit_port_mappings = [
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
  ]
}

data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_ecr_repository" "app" {
  name                 = var.name_prefix
  image_tag_mutability = "MUTABLE"
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
  cidr_block           = "10.80.0.0/16"
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
  cidr_block              = cidrsubnet(aws_vpc.this.cidr_block, 8, count.index)
  map_public_ip_on_launch = true
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
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
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
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
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

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "livekit" {
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

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_lb" "app" {
  name               = "${var.name_prefix}-app"
  load_balancer_type = "application"
  security_groups    = [aws_security_group.app_alb.id]
  subnets            = aws_subnet.public[*].id
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
  name               = "${var.name_prefix}-livekit"
  load_balancer_type = "network"
  subnets            = aws_subnet.public[*].id
}

resource "aws_lb_target_group" "livekit_signal" {
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

resource "aws_lb_listener" "livekit_signal_tcp" {
  count = var.livekit_certificate_arn == "" ? 1 : 0

  load_balancer_arn = aws_lb.livekit.arn
  port              = var.livekit_signal_port
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_signal.arn
  }
}

resource "aws_lb_listener" "livekit_signal_tls" {
  count = var.livekit_certificate_arn == "" ? 0 : 1

  load_balancer_arn = aws_lb.livekit.arn
  port              = var.livekit_signal_port
  protocol          = "TLS"
  certificate_arn   = var.livekit_certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_signal.arn
  }
}

resource "aws_lb_listener" "livekit_tcp" {
  load_balancer_arn = aws_lb.livekit.arn
  port              = var.livekit_tcp_port
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_tcp.arn
  }
}

resource "aws_lb_listener" "livekit_udp" {
  load_balancer_arn = aws_lb.livekit.arn
  port              = var.livekit_udp_port
  protocol          = "UDP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.livekit_udp.arn
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
  subnet_id       = aws_subnet.public[count.index].id
  security_groups = [aws_security_group.efs.id]
}

resource "aws_ecs_cluster" "this" {
  name = var.name_prefix

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
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

resource "aws_iam_role_policy_attachment" "task_execution" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "task_execution_secrets" {
  name = "${var.name_prefix}-secrets"
  role = aws_iam_role.task_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = distinct(concat(local.app_secret_arns, [var.livekit_config_secret_arn]))
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
        Effect   = "Allow"
        Action   = ["bedrock:InvokeModel"]
        Resource = var.bedrock_model_arns
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
      }
    }
  }

  container_definitions = jsonencode([
    {
      name      = local.app_container_name
      image     = local.app_image
      essential = true

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

      secrets = [
        {
          name      = "LIVEKIT_CONFIG"
          valueFrom = var.livekit_config_secret_arn
        }
      ]

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
  name            = "${var.name_prefix}-livekit"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.livekit.arn
  desired_count   = var.livekit_desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.public[*].id
    security_groups  = [aws_security_group.livekit.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.livekit_signal.arn
    container_name   = local.livekit_container_name
    container_port   = var.livekit_signal_port
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.livekit_tcp.arn
    container_name   = local.livekit_container_name
    container_port   = var.livekit_tcp_port
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.livekit_udp.arn
    container_name   = local.livekit_container_name
    container_port   = var.livekit_udp_port
  }
}

resource "aws_ecs_service" "app" {
  name            = "${var.name_prefix}-app"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = var.app_desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.public[*].id
    security_groups  = [aws_security_group.app_task.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = local.app_container_name
    container_port   = local.app_port
  }

  depends_on = [
    aws_lb_listener.app_http_forward,
    aws_lb_listener.app_http_redirect,
    aws_lb_listener.livekit_signal_tcp,
    aws_lb_listener.livekit_signal_tls,
    aws_efs_mount_target.board,
    aws_ecs_service.livekit,
  ]
}
