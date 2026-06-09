# Edge authentication: ACM certificate, Cognito user pool with Google
# federation, and the ALB authenticate-cognito flow. Enabled when
# var.auth_domain_name is set (e.g. meet.sc.tt). When empty, the module falls
# back to the plain-HTTP app listener defined in app.tf (dev/no-domain mode).

locals {
  auth_enabled = var.auth_domain_name != ""
}

# ---------------------------------------------------------------------------
# ACM certificate (DNS-validated). The validation CNAME is an output the
# operator pastes into their DNS provider (Cloudflare for sc.tt).
# ---------------------------------------------------------------------------
resource "aws_acm_certificate" "app" {
  count = local.auth_enabled ? 1 : 0

  domain_name       = var.auth_domain_name
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

# Waits until the DNS validation record is live. Because DNS is managed
# externally (Cloudflare), this will block on first apply until the operator
# adds the CNAME from the acm_validation_record output, then re-run apply.
resource "aws_acm_certificate_validation" "app" {
  count = local.auth_enabled && var.acm_wait_for_validation ? 1 : 0

  certificate_arn = aws_acm_certificate.app[0].arn
}

# ---------------------------------------------------------------------------
# Cognito user pool + Hosted UI domain + Google identity provider + app client.
# ---------------------------------------------------------------------------
resource "aws_cognito_user_pool" "this" {
  count = local.auth_enabled ? 1 : 0

  name                     = var.name_prefix
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  admin_create_user_config {
    allow_admin_create_user_only = false
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }
}

resource "aws_cognito_user_pool_domain" "this" {
  count = local.auth_enabled ? 1 : 0

  domain       = var.cognito_domain_prefix
  user_pool_id = aws_cognito_user_pool.this[0].id
}

# Google federation. The client id/secret come from variables sourced from
# Secrets Manager / env (never hardcoded). Created only when both are provided.
resource "aws_cognito_identity_provider" "google" {
  count = local.auth_enabled && var.google_client_id != "" ? 1 : 0

  user_pool_id  = aws_cognito_user_pool.this[0].id
  provider_name = "Google"
  provider_type = "Google"

  provider_details = {
    client_id        = var.google_client_id
    client_secret    = var.google_client_secret
    authorize_scopes = "openid email profile"
  }

  # Map Google claims to Cognito attributes; email is what the app's allowlist
  # keys on, so it must be present.
  attribute_mapping = {
    email          = "email"
    email_verified = "email_verified"
    username       = "sub"
    name           = "name"
  }
}

# Microsoft (Azure AD) federation via a generic OIDC provider against the Azure
# AD v2 endpoints. provider_name "Microsoft" is referenced by the client below.
# Created only when client id/secret + tenant are provided.
resource "aws_cognito_identity_provider" "microsoft" {
  count = local.auth_enabled && var.microsoft_client_id != "" ? 1 : 0

  user_pool_id  = aws_cognito_user_pool.this[0].id
  provider_name = "Microsoft"
  provider_type = "OIDC"

  provider_details = {
    client_id                 = var.microsoft_client_id
    client_secret             = var.microsoft_client_secret
    authorize_scopes          = "openid email profile"
    oidc_issuer               = "https://login.microsoftonline.com/${var.microsoft_tenant_id}/v2.0"
    attributes_request_method = "GET"
    # Azure AD v2 publishes a discovery document at the issuer; Cognito reads the
    # authorize/token/jwks/userinfo endpoints from it.
  }

  attribute_mapping = {
    email    = "email"
    username = "sub"
    name     = "name"
  }
}

resource "aws_cognito_user_pool_client" "this" {
  count = local.auth_enabled ? 1 : 0

  name         = "${var.name_prefix}-alb"
  user_pool_id = aws_cognito_user_pool.this[0].id

  generate_secret                      = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_scopes                 = ["openid", "email", "profile"]

  # Google + Microsoft when configured. COGNITO is always included so the client
  # never has an empty provider list (Cognito rejects that) and stays valid
  # during the bootstrap window before federated credentials are applied. The
  # app's email allowlist gates access regardless of provider.
  supported_identity_providers = compact([
    var.google_client_id != "" ? "Google" : "",
    var.microsoft_client_id != "" ? "Microsoft" : "",
    "COGNITO",
  ])

  # The ALB's reserved callback path on the app domain.
  callback_urls = ["https://${var.auth_domain_name}/oauth2/idpresponse"]
  logout_urls   = ["https://${var.auth_domain_name}/"]

  depends_on = [
    aws_cognito_identity_provider.google,
    aws_cognito_identity_provider.microsoft,
  ]
}
