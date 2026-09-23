# Container registry, cluster, task definitions and services.

resource "aws_ecr_repository" "image" {
  for_each             = toset(["api", "marketing", "admin"])
  name                 = "${local.name}-${each.key}"
  image_tag_mutability = "IMMUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "image" {
  for_each   = aws_ecr_repository.image
  repository = each.value.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep the 20 most recent images"
      selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 20 }
      action       = { type = "expire" }
    }]
  })
}

resource "aws_ecs_cluster" "main" {
  name = local.name

  setting {
    name  = "containerInsights"
    value = local.is_production ? "enabled" : "disabled"
  }
}

resource "aws_cloudwatch_log_group" "service" {
  for_each          = toset(["api", "worker", "marketing", "admin", "migrate"])
  name              = "/mise/${var.environment}/${each.key}"
  retention_in_days = var.log_retention_days
}

# ---------- IAM ----------

data "aws_iam_policy_document" "ecs_tasks_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

# Used by ECS itself to pull images, write logs and read the secrets it injects.
resource "aws_iam_role" "execution" {
  name               = "${local.name}-ecs-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_tasks_assume.json
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "execution_secrets" {
  name = "read-application-secrets"
  role = aws_iam_role.execution.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = "secretsmanager:GetSecretValue"
      Resource = [
        aws_secretsmanager_secret.token_signing_key.arn,
        aws_secretsmanager_secret.smtp.arn,
        aws_db_instance.main.master_user_secret[0].secret_arn,
      ]
    }]
  })
}

# The role the application code runs as. It needs no AWS permissions today:
# email goes over SMTP and the database over the network.
resource "aws_iam_role" "task" {
  name               = "${local.name}-ecs-task"
  assume_role_policy = data.aws_iam_policy_document.ecs_tasks_assume.json
}

# ---------- Task definitions ----------

locals {
  db_secret_arn = aws_db_instance.main.master_user_secret[0].secret_arn

  api_environment = [
    { name = "MISE_ENV", value = var.environment },
    { name = "HTTP_ADDR", value = ":8080" },
    { name = "LOG_LEVEL", value = "info" },
    { name = "DB_HOST", value = aws_db_instance.main.address },
    { name = "DB_NAME", value = aws_db_instance.main.db_name },
    { name = "DB_SSLMODE", value = "require" },
    { name = "PUBLIC_SITE_URL", value = "https://${local.site_host}" },
    { name = "CORS_ORIGINS", value = "https://${local.site_host}" },
    # One proxy, the load balancer, appends the caller's address to X-Forwarded-For.
    { name = "TRUSTED_PROXY_HOPS", value = "1" },
    { name = "EMAIL_VERIFICATION_REQUIRED", value = tostring(var.email_verification_required) },
    { name = "REFERRAL_MILESTONES", value = var.referral_milestones },
    { name = "MAIL_DRIVER", value = "smtp" },
    { name = "SMTP_HOST", value = "email-smtp.${data.aws_region.current.name}.amazonaws.com" },
    { name = "SMTP_PORT", value = "587" },
    { name = "SMTP_STARTTLS", value = "true" },
    { name = "MAIL_FROM", value = var.mail_from },
  ]

  api_secrets = [
    { name = "DB_USER", valueFrom = "${local.db_secret_arn}:username::" },
    { name = "DB_PASSWORD", valueFrom = "${local.db_secret_arn}:password::" },
    { name = "TOKEN_SIGNING_KEY", valueFrom = aws_secretsmanager_secret.token_signing_key.arn },
    { name = "SMTP_USERNAME", valueFrom = "${aws_secretsmanager_secret.smtp.arn}:username::" },
    { name = "SMTP_PASSWORD", valueFrom = "${aws_secretsmanager_secret.smtp.arn}:password::" },
  ]

  # name => container settings. Images start as a placeholder; the deploy
  # workflow registers new revisions with the real image tag.
  tasks = {
    api = {
      repository  = "api"
      command     = ["/app/api"]
      port        = 8080
      cpu         = 256
      memory      = 512
      environment = local.api_environment
      secrets     = local.api_secrets
    }
    worker = {
      repository  = "api"
      command     = ["/app/worker"]
      port        = null
      cpu         = 256
      memory      = 512
      environment = local.api_environment
      secrets     = local.api_secrets
    }
    migrate = {
      repository  = "api"
      command     = ["/app/misectl", "migrate", "up"]
      port        = null
      cpu         = 256
      memory      = 512
      environment = local.api_environment
      secrets     = local.api_secrets
    }
    marketing = {
      repository = "marketing"
      command    = null
      port       = 3100
      cpu        = 256
      memory     = 512
      environment = [
        { name = "PORT", value = "3100" },
        { name = "API_INTERNAL_URL", value = "https://${local.api_host}" },
      ]
      secrets = []
    }
    admin = {
      repository = "admin"
      command    = null
      port       = 3101
      cpu        = 256
      memory     = 512
      environment = [
        { name = "PORT", value = "3101" },
        { name = "API_INTERNAL_URL", value = "https://${local.api_host}" },
        { name = "ADMIN_COOKIE_SECURE", value = "true" },
      ]
      secrets = []
    }
  }
}

resource "aws_ecs_task_definition" "task" {
  for_each                 = local.tasks
  family                   = "${local.name}-${each.key}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = each.value.cpu
  memory                   = each.value.memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn

  runtime_platform {
    cpu_architecture        = "X86_64"
    operating_system_family = "LINUX"
  }

  container_definitions = jsonencode([{
    name                   = each.key
    image                  = "${aws_ecr_repository.image[each.value.repository].repository_url}:bootstrap"
    essential              = true
    command                = each.value.command
    environment            = each.value.environment
    secrets                = each.value.secrets
    readonlyRootFilesystem = each.value.repository == "api"
    portMappings           = each.value.port == null ? [] : [{ containerPort = each.value.port, protocol = "tcp" }]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.service[each.key].name
        awslogs-region        = data.aws_region.current.name
        awslogs-stream-prefix = each.key
      }
    }
  }])

  # The deploy workflow owns the image tag from here on.
  lifecycle {
    ignore_changes = [container_definitions]
  }
}

# ---------- Services ----------

resource "aws_ecs_service" "service" {
  for_each        = { for name, task in local.tasks : name => task if name != "migrate" }
  name            = "${local.name}-${each.key}"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.task[each.key].arn
  desired_count   = var.desired_count[each.key]
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.public[*].id
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = true
  }

  dynamic "load_balancer" {
    for_each = each.value.port == null ? [] : [1]
    content {
      target_group_arn = aws_lb_target_group.service[each.key].arn
      container_name   = each.key
      container_port   = each.value.port
    }
  }

  # A deployment whose tasks fail their health checks is rolled back automatically.
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  deployment_minimum_healthy_percent = 100
  deployment_maximum_percent         = 200
  health_check_grace_period_seconds  = each.value.port == null ? null : 30

  lifecycle {
    ignore_changes = [task_definition]
  }

  depends_on = [aws_lb_listener_rule.service]
}
