# EFS-backed /srv/data for the app's SQLite board snapshot and event store.
# Mount targets live in the existing public subnets, reachable only from the
# app task security group.

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
  for_each = toset(local.public_subnet_ids)

  file_system_id  = aws_efs_file_system.board.id
  subnet_id       = each.value
  security_groups = [aws_security_group.efs.id]
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
