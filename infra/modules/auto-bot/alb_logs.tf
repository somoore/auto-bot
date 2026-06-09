# ALB access logs — used to diagnose authenticate-cognito failures (the
# error_reason field is only visible here). Gated on var.enable_alb_access_logs
# so it can be turned on for debugging and off afterwards.

data "aws_elb_service_account" "main" {
  count = var.enable_alb_access_logs ? 1 : 0
}

resource "aws_s3_bucket" "alb_logs" {
  count         = var.enable_alb_access_logs ? 1 : 0
  bucket        = "${var.name_prefix}-alb-logs-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket_policy" "alb_logs" {
  count  = var.enable_alb_access_logs ? 1 : 0
  bucket = aws_s3_bucket.alb_logs[0].id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { AWS = data.aws_elb_service_account.main[0].arn }
        Action    = "s3:PutObject"
        Resource  = "${aws_s3_bucket.alb_logs[0].arn}/*"
      }
    ]
  })
}
