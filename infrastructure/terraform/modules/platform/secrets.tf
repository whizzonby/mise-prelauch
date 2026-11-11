# Application secrets live in Secrets Manager and reach containers as
# environment variables injected by ECS at start. Nothing secret is written
# into a task definition, an image or this repository.

resource "random_password" "token_signing_key" {
  length  = 64
  special = false
}

resource "aws_secretsmanager_secret" "token_signing_key" {
  name_prefix = "${local.name}/token-signing-key-"
  description = "Signs lead verification, profile and unsubscribe tokens. Rotating it invalidates every outstanding email link."
}

resource "aws_secretsmanager_secret_version" "token_signing_key" {
  secret_id     = aws_secretsmanager_secret.token_signing_key.id
  secret_string = random_password.token_signing_key.result
}

resource "aws_secretsmanager_secret" "smtp" {
  name_prefix = "${local.name}/ses-smtp-"
  description = "SES SMTP credentials used by the worker to send transactional email."
}

resource "aws_secretsmanager_secret_version" "smtp" {
  secret_id = aws_secretsmanager_secret.smtp.id
  secret_string = jsonencode({
    username = aws_iam_access_key.ses_smtp.id
    password = aws_iam_access_key.ses_smtp.ses_smtp_password_v4
  })
}

# Sprint 1: seals TOTP secrets in the database (AES-256-GCM). 32 random bytes,
# base64. Losing it makes every stored MFA secret unreadable (admins re-enrol);
# it is never rotated by Terraform.
resource "random_bytes" "data_encryption_key" {
  length = 32
}

resource "aws_secretsmanager_secret" "data_encryption_key" {
  name_prefix = "${local.name}/data-encryption-key-"
  description = "Seals MFA secrets stored in the database."
}

resource "aws_secretsmanager_secret_version" "data_encryption_key" {
  secret_id     = aws_secretsmanager_secret.data_encryption_key.id
  secret_string = random_bytes.data_encryption_key.base64
}

# Shared by the API and the Next.js servers (admin, later web), so the API can
# believe the end-user address they pass on (docs/security/AUTHENTICATION.md).
resource "random_password" "bff_shared_key" {
  length  = 64
  special = false
}

resource "aws_secretsmanager_secret" "bff_shared_key" {
  name_prefix = "${local.name}/bff-shared-key-"
  description = "Authenticates the Next.js servers when they forward the end user's address to the API."
}

resource "aws_secretsmanager_secret_version" "bff_shared_key" {
  secret_id     = aws_secretsmanager_secret.bff_shared_key.id
  secret_string = random_password.bff_shared_key.result
}
