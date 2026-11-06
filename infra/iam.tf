# devctl finds this policy by name and attaches it to every per-space role.
resource "aws_iam_policy" "space_boundary" {
  name        = var.domain
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
        # A space's secrets live at /<space domain>/<app>, and every space
        # domain is one label under the root, so `*.<domain>/*` covers every
        # space's every app and nothing else. Read-only: devctl is the only
        # writer of secrets.
        Resource = "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter/*.${var.domain}/*"
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
            "kms:ViaService" = "ssm.${var.region}.amazonaws.com"
          }
        }
      },
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          # Litestream's own retention prunes a space's replicas; the
          # per-space policy narrows this to the space's own prefix.
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
        Action = "route53:ChangeResourceRecordSets"
        # The one zone. Record types A and TXT only: a space writes its own
        # <space> and *.<space> A records and its ACME challenge TXT records,
        # and the apex holder also writes the TXT at _acme-challenge.<domain>.
        # The per-space inline policy narrows to those names; no space role
        # can write an NS record.
        Resource = aws_route53_zone.root.arn
        Condition = {
          "ForAllValues:StringEquals" = {
            "route53:ChangeResourceRecordSetsRecordTypes" = ["A", "TXT"]
          }
        }
      },
      {
        Effect   = "Allow"
        Action   = "route53:ListResourceRecordSets"
        Resource = aws_route53_zone.root.arn
      },
      {
        Effect   = "Allow"
        Action   = "route53:GetChange"
        Resource = "arn:aws:route53:::change/*"
      },
      {
        # Session Manager: the SSM agent registers the instance and carries a
        # session, so an operator can reach a host without sshd. These are the
        # session actions of AmazonSSMManagedInstanceCore and none of its
        # broader ssm reads.
        Effect   = "Allow"
        Action   = "ssm:UpdateInstanceInformation"
        Resource = "arn:aws:ec2:${var.region}:${data.aws_caller_identity.current.account_id}:instance/*"
      },
      {
        # ssmmessages and ec2messages have no resource-level permissions.
        Effect = "Allow"
        Action = [
          "ssmmessages:CreateControlChannel",
          "ssmmessages:CreateDataChannel",
          "ssmmessages:OpenControlChannel",
          "ssmmessages:OpenDataChannel",
          "ec2messages:AcknowledgeMessage",
          "ec2messages:DeleteMessage",
          "ec2messages:FailMessage",
          "ec2messages:GetEndpoint",
          "ec2messages:GetMessages",
          "ec2messages:SendReply",
        ]
        Resource = "*"
      },
    ]
  })
}
