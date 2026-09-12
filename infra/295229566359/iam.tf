resource "aws_iam_policy" "space_boundary" {
  name        = "ikigenba-space-boundary"
  description = "Permissions boundary for per-space instance roles; the ceiling any space role can hold."
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ssm:GetParameter",
          "ssm:GetParameters",
          "ssm:GetParametersByPath",
          "ssm:PutParameter",
          "ssm:DeleteParameter",
          "ssm:DeleteParameters",
        ]
        # Two wildcard segments: /ikigenba/<space>/<key>. /ikigenba/account is out of reach.
        Resource = "arn:aws:ssm:us-east-2:295229566359:parameter/ikigenba/*/*"
      },
      {
        Effect = "Allow"
        Action = [
          "kms:Decrypt",
          "kms:GenerateDataKey",
        ]
        Resource = "*"
        Condition = {
          StringEquals = {
            "kms:ViaService" = "ssm.us-east-2.amazonaws.com"
          }
        }
      },
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
        ]
        Resource = "${aws_s3_bucket.backups.arn}/*"
      },
      {
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = aws_s3_bucket.backups.arn
      },
      {
        Effect = "Allow"
        Action = [
          "route53:ChangeResourceRecordSets",
          "route53:ListResourceRecordSets",
        ]
        # Any hosted zone in the account: a space's domain may sit in the
        # ikigenba.dev zone or in any other zone this account owns, and the
        # per-space inline policy narrows to the one zone that space's domain
        # resolves to.
        Resource = "arn:aws:route53:::hostedzone/*"
      },
      {
        Effect   = "Allow"
        Action   = "route53:GetChange"
        Resource = "arn:aws:route53:::change/*"
      },
    ]
  })
}
