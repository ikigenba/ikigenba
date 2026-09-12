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
        ]
        # `*` matches `/`, so parameter/ikigenba/*/* covers every path under
        # /ikigenba/ that has at least two more segments — every space's
        # /ikigenba/<domain>/<app> — and excludes only /ikigenba/account (one
        # segment). Read-only: the operator-side tool is the only writer of
        # secrets.
        Resource = "arn:aws:ssm:us-east-2:602773793009:parameter/ikigenba/*/*"
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
        Action = "route53:ChangeResourceRecordSets"
        # Any hosted zone in the account: a space's domain may sit in the sbx
        # zone or in any other zone this account owns, and the per-space inline
        # policy narrows to the one zone that space's domain resolves to.
        # Record types A and TXT only: no space role can rewrite an NS
        # delegation.
        Resource = "arn:aws:route53:::hostedzone/*"
        Condition = {
          "ForAllValues:StringEquals" = {
            "route53:ChangeResourceRecordSetsRecordTypes" = ["A", "TXT"]
          }
        }
      },
      {
        Effect   = "Allow"
        Action   = "route53:ListResourceRecordSets"
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
