# devctl finds this policy by name and attaches it to every per-space role.
resource "aws_iam_policy" "space_boundary" {
  name        = var.domain
  description = "Permissions boundary for per-space instance roles; the ceiling any space role can hold."
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat([
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
    ], local.session_manager_statements, local.hosts_operator_statements)
  })
}

# Session Manager: the SSM agent registers the instance and carries a session,
# so an operator can reach a host without sshd. These are the session actions
# of AmazonSSMManagedInstanceCore and none of its broader ssm reads. Spaces
# get them through the boundary; SSH hosts hold them and nothing else.
locals {
  session_manager_statements = [
    {
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
}

# SSH hosts' instance role and profile. accounts passes the profile at launch
# (the hosts' launch template carries none). Session Manager only: no S3, no
# secrets, no Route 53. accountsd proves who it is with a presigned
# sts:GetCallerIdentity, which needs no permission.
resource "aws_iam_role" "hosts" {
  name        = "${var.domain}-hosts"
  description = "Instance role of the SSH hosts accounts runs: Session Manager only."
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { Service = "ec2.amazonaws.com" }
        Action    = "sts:AssumeRole"
      },
    ]
  })
}

resource "aws_iam_role_policy" "hosts_session_manager" {
  name = "session-manager"
  role = aws_iam_role.hosts.id
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.session_manager_statements
  })
}

resource "aws_iam_instance_profile" "hosts" {
  name = aws_iam_role.hosts.name
  role = aws_iam_role.hosts.name
}

# What accounts on the hosts-operator space calls, and nothing else. devctl
# finds this policy by name and attaches it to that one space's role (`devctl
# hosts set`); the boundary holds the same statements so the attachment takes
# effect. Every write is scoped by the Kind tag accounts puts on what it
# creates, in the same call that creates it: Kind=host on an instance and the
# root volume and network interface it launches with, and on an Elastic IP;
# Kind=host-data on a data volume. Nothing without one of those tags can be
# stopped, changed, attached to or deleted, and a tag can be added after
# creation only to something that already carries one, so no space's
# resources are reachable.
locals {
  ec2_arn = "arn:aws:ec2:${var.region}:${data.aws_caller_identity.current.account_id}"
  # The arm64 families outcome 16 allows, so the daemon is one build.
  host_instance_types = ["t4g.*", "m7g.*", "c7g.*", "r7g.*"]

  hosts_operator_statements = [
    {
      # Launch from the hosts' template, into the hosts' security group, with
      # an Amazon image and an arm64 type of the allowed families.
      Effect   = "Allow"
      Action   = "ec2:RunInstances"
      Resource = "${local.ec2_arn}:instance/*"
      Condition = {
        StringEquals = {
          "aws:RequestTag/Kind" = "host"
          "ec2:LaunchTemplate"  = aws_launch_template.hosts.arn
        }
        StringLike = { "ec2:InstanceType" = local.host_instance_types }
      }
    },
    {
      Effect = "Allow"
      Action = "ec2:RunInstances"
      Resource = [
        "${local.ec2_arn}:volume/*",
        "${local.ec2_arn}:network-interface/*",
      ]
      Condition = {
        StringEquals = { "aws:RequestTag/Kind" = "host" }
      }
    },
    {
      Effect = "Allow"
      Action = "ec2:RunInstances"
      Resource = [
        aws_launch_template.hosts.arn,
        aws_security_group.hosts.arn,
        "${local.ec2_arn}:subnet/*",
      ]
    },
    {
      Effect   = "Allow"
      Action   = "ec2:RunInstances"
      Resource = "arn:aws:ec2:${var.region}::image/*"
      Condition = {
        StringEquals = { "ec2:Owner" = "amazon" }
      }
    },
    {
      # The image's own snapshot, read at launch. Unconditioned: the image is
      # the condition that matters, and this account holds no EBS snapshots.
      Effect   = "Allow"
      Action   = "ec2:RunInstances"
      Resource = "arn:aws:ec2:${var.region}::snapshot/*"
    },
    {
      Effect   = "Allow"
      Action   = "iam:PassRole"
      Resource = aws_iam_role.hosts.arn
      Condition = {
        StringEquals = { "iam:PassedToService" = "ec2.amazonaws.com" }
      }
    },
    {
      Effect = "Allow"
      Action = [
        "ec2:StopInstances",
        "ec2:StartInstances",
        "ec2:TerminateInstances",
        "ec2:ModifyInstanceAttribute",
      ]
      Resource = "${local.ec2_arn}:instance/*"
      Condition = {
        StringEquals = { "aws:ResourceTag/Kind" = "host" }
        # A resize stays inside the same families.
        StringLikeIfExists = { "ec2:Attribute/InstanceType" = local.host_instance_types }
      }
    },
    {
      Effect   = "Allow"
      Action   = "ec2:CreateVolume"
      Resource = "${local.ec2_arn}:volume/*"
      Condition = {
        StringEquals = { "aws:RequestTag/Kind" = "host-data" }
      }
    },
    {
      # Attach and detach take the volume and the instance; modify and delete
      # take the volume, root or data.
      Effect = "Allow"
      Action = [
        "ec2:AttachVolume",
        "ec2:DetachVolume",
        "ec2:ModifyVolume",
        "ec2:DeleteVolume",
      ]
      Resource = [
        "${local.ec2_arn}:instance/*",
        "${local.ec2_arn}:volume/*",
      ]
      Condition = {
        StringEquals = { "aws:ResourceTag/Kind" = ["host", "host-data"] }
      }
    },
    {
      Effect   = "Allow"
      Action   = "ec2:AllocateAddress"
      Resource = "${local.ec2_arn}:elastic-ip/*"
      Condition = {
        StringEquals = { "aws:RequestTag/Kind" = "host" }
      }
    },
    {
      # Associate takes the address and the instance or its network
      # interface; disassociate the address and the interface; release the
      # address. All of them carry Kind=host from launch or allocation.
      Effect = "Allow"
      Action = [
        "ec2:AssociateAddress",
        "ec2:DisassociateAddress",
        "ec2:ReleaseAddress",
      ]
      Resource = [
        "${local.ec2_arn}:elastic-ip/*",
        "${local.ec2_arn}:instance/*",
        "${local.ec2_arn}:network-interface/*",
      ]
      Condition = {
        StringEquals = { "aws:ResourceTag/Kind" = "host" }
      }
    },
    {
      # Tags in the call that creates the resource.
      Effect   = "Allow"
      Action   = "ec2:CreateTags"
      Resource = "*"
      Condition = {
        StringEquals = { "ec2:CreateAction" = ["RunInstances", "CreateVolume", "AllocateAddress"] }
        # Space is devctl's registry tag; nothing accounts makes carries it.
        "ForAllValues:StringNotEquals" = { "aws:TagKeys" = ["Space"] }
      }
    },
    {
      # Tags added later, such as marking a data volume orphaned, only on what
      # already carries a hosts Kind; the Kind itself never changes, so
      # nothing is adopted or released by retagging.
      Effect = "Allow"
      Action = "ec2:CreateTags"
      Resource = [
        "${local.ec2_arn}:instance/*",
        "${local.ec2_arn}:volume/*",
        "${local.ec2_arn}:elastic-ip/*",
      ]
      Condition = {
        StringEquals                   = { "aws:ResourceTag/Kind" = ["host", "host-data"] }
        "ForAllValues:StringNotEquals" = { "aws:TagKeys" = ["Kind", "Space", "Domain"] }
      }
    },
    {
      # Reads take no resource.
      Effect = "Allow"
      Action = [
        "ec2:DescribeAddresses",
        "ec2:DescribeAvailabilityZones",
        "ec2:DescribeImages",
        "ec2:DescribeInstanceAttribute",
        "ec2:DescribeInstanceStatus",
        "ec2:DescribeInstanceTypes",
        "ec2:DescribeInstances",
        "ec2:DescribeLaunchTemplateVersions",
        "ec2:DescribeLaunchTemplates",
        "ec2:DescribeSubnets",
        "ec2:DescribeTags",
        "ec2:DescribeVolumes",
        "ec2:DescribeVolumesModifications",
      ]
      Resource = "*"
    },
    {
      # The hosts zone, A records below the apex only, never the site names
      # Terraform keeps: accounts writes <account>.<hosts domain> and nothing
      # else.
      Effect   = "Allow"
      Action   = "route53:ChangeResourceRecordSets"
      Resource = aws_route53_zone.hosts.arn
      Condition = {
        "ForAllValues:StringEquals" = {
          "route53:ChangeResourceRecordSetsRecordTypes" = ["A"]
        }
        "ForAllValues:StringLike" = {
          "route53:ChangeResourceRecordSetsNormalizedRecordNames" = ["*.${local.hosts_domain}"]
        }
        "ForAllValues:StringNotEquals" = {
          "route53:ChangeResourceRecordSetsNormalizedRecordNames" = [for r in aws_route53_record.hosts_site : r.name]
        }
      }
    },
    {
      Effect   = "Allow"
      Action   = "route53:ListResourceRecordSets"
      Resource = aws_route53_zone.hosts.arn
    },
    {
      Effect   = "Allow"
      Action   = "route53:GetChange"
      Resource = "arn:aws:route53:::change/*"
    },
  ]
}

resource "aws_iam_policy" "hosts_operator" {
  name        = "${var.domain}-hosts-operator"
  description = "What accounts on the one hosts-operator space needs to run SSH hosts; devctl attaches it."
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.hosts_operator_statements
  })
}
