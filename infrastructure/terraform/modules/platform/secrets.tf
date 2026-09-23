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
