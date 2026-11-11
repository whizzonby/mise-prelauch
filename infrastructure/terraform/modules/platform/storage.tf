# ---------- Media storage: meal and chef photographs ----------
#
# Approved 11 November 2026 (Sprint 2 report, decision 5). A private bucket,
# readable only through the site's CloudFront distribution at
# https://<domain>/media/*. Nothing is public on the bucket itself.
#
# Uploading from the admin is not built yet, so no service is granted write
# access (least privilege). Until it is, an operator copies photographs in:
#   aws s3 cp meal.jpg s3://<bucket>/media/catalog/meal.jpg --cache-control "public, max-age=31536000, immutable"
# and staff reference them as https://<domain>/media/catalog/meal.jpg.
# Use a new file name for a changed photograph; cached copies never expire.
#
# Estimated cost at launch volume (a few hundred photographs, about 100 MB):
# under US$1 a month for storage and requests. Delivery is billed as part of
# the existing CloudFront distribution.

resource "aws_s3_bucket" "media" {
  bucket = "${local.name}-media"
}

resource "aws_s3_bucket_public_access_block" "media" {
  bucket                  = aws_s3_bucket.media.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Keeps replaced or deleted photographs recoverable for 30 days.
resource "aws_s3_bucket_versioning" "media" {
  bucket = aws_s3_bucket.media.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    id     = "expire-old-versions"
    status = "Enabled"
    filter {}
    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}

resource "aws_cloudfront_origin_access_control" "media" {
  name                              = "${local.name}-media"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# Only the site distribution may read, and only objects under media/.
data "aws_iam_policy_document" "media_read" {
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.media.arn}/media/*"]
    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.site.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "media" {
  bucket = aws_s3_bucket.media.id
  policy = data.aws_iam_policy_document.media_read.json
}
