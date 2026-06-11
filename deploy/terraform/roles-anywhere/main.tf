# IAM Roles Anywhere for auto-bot — lets a Kubernetes pod call AWS Bedrock with
# NO long-lived AWS access key. The pod presents an X.509 client certificate
# (signed by a CA you control) and exchanges it for short-lived STS credentials.
#
# This module creates:
#   - a trust anchor (your CA cert)
#   - an IAM role scoped to exactly the Bedrock actions auto-bot needs
#   - a Roles Anywhere profile linking them
#
# You supply your CA certificate (PEM). Generate a CA + leaf cert with the helper
# script in this directory (gen-certs.sh), store the leaf in a Kubernetes Secret,
# and point the Helm chart's awsRolesAnywhere.* values at the outputs below.

terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

data "aws_caller_identity" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
}

# --- Trust anchor: your CA certificate ---------------------------------------
resource "aws_rolesanywhere_trust_anchor" "this" {
  name    = "${var.name_prefix}-trust-anchor"
  enabled = true
  source {
    source_type = "CERTIFICATE_BUNDLE"
    source_data {
      x509_certificate_data = var.ca_certificate_pem
    }
  }
}

# --- IAM role the pod assumes (least privilege) ------------------------------
data "aws_iam_policy_document" "trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole", "sts:TagSession", "sts:SetSourceIdentity"]
    principals {
      type        = "Service"
      identifiers = ["rolesanywhere.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_rolesanywhere_trust_anchor.this.arn]
    }
    # Bind to the leaf certificate's subject CN so only your pod's cert qualifies.
    condition {
      test     = "StringEquals"
      variable = "aws:PrincipalTag/x509Subject/CN"
      values   = [var.certificate_cn]
    }
  }
}

resource "aws_iam_role" "bedrock" {
  name                 = "${var.name_prefix}-bedrock"
  description          = "auto-bot pod -> Bedrock via IAM Roles Anywhere"
  assume_role_policy   = data.aws_iam_policy_document.trust.json
  max_session_duration = 3600
}

data "aws_iam_policy_document" "bedrock" {
  # Claude agent runs use inference profiles, which require permission on both the
  # profile ARN and the underlying foundation-model ARNs across routed regions.
  statement {
    sid       = "InvokeAgentModels"
    effect    = "Allow"
    actions   = ["bedrock:InvokeModel", "bedrock:InvokeModelWithResponseStream"]
    resources = var.agent_model_arns
  }
  # Nova Sonic speech-to-speech uses the bidirectional streaming action.
  statement {
    sid    = "InvokeNovaSonic"
    effect = "Allow"
    actions = [
      "bedrock:InvokeModel",
      "bedrock:InvokeModelWithResponseStream",
      "bedrock:InvokeModelWithBidirectionalStream",
    ]
    resources = var.nova_sonic_model_arns
  }
}

resource "aws_iam_role_policy" "bedrock" {
  name   = "bedrock-invoke"
  role   = aws_iam_role.bedrock.id
  policy = data.aws_iam_policy_document.bedrock.json
}

# --- Roles Anywhere profile (links the role) ---------------------------------
resource "aws_rolesanywhere_profile" "this" {
  name             = "${var.name_prefix}-profile"
  role_arns        = [aws_iam_role.bedrock.arn]
  duration_seconds = 3600
  enabled          = true
}
