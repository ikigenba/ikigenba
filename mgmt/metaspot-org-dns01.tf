# Cross-account DNS-01 role for the int box.
#
# The int box (int/ account 704229156466) serves metaspot.org as a sites
# custom domain and must issue a wildcard cert (metaspot.org + *.metaspot.org),
# which Let's Encrypt only grants via DNS-01. The metaspot.org zone lives here
# in mgmt, so the box cannot be granted access with an inline policy on its own
# instance role — Route 53 zones are account-scoped. Instead this role, owned by
# mgmt, trusts the int instance role and carries the zone write; certbot-dns-
# route53 on the box assumes it to plant the _acme-challenge TXT records.
#
# The int instance-role ARN is a hardcoded literal, the same cross-account
# discipline parked.tf uses: this repo never wires a terraform_remote_state
# cross-account read. If the int role is ever renamed or its account changes,
# update this literal.
resource "aws_iam_role" "metaspot_org_dns01" {
  name = "metaspot-org-dns01"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { AWS = "arn:aws:iam::704229156466:role/int" }
    }]
  })
}

# Write access to the metaspot.org zone, constrained to TXT records — all
# certbot-dns-route53 ever touches for a DNS-01 challenge. GetChange has no
# resource-level scoping in Route 53 and takes a change-id ARN.
resource "aws_iam_role_policy" "metaspot_org_dns01" {
  name = "route53-dns01"
  role = aws_iam_role.metaspot_org_dns01.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = "route53:ChangeResourceRecordSets"
        Resource = "arn:aws:route53:::hostedzone/${aws_route53_zone.metaspot_org.zone_id}"
        Condition = {
          "ForAllValues:StringEquals" = {
            "route53:ChangeResourceRecordSetsRecordTypes" = ["TXT"]
          }
        }
      },
      {
        Effect   = "Allow"
        Action   = "route53:GetChange"
        Resource = "arn:aws:route53:::change/*"
      },
      {
        Effect   = "Allow"
        Action   = "route53:ListHostedZones"
        Resource = "*"
      },
    ]
  })
}
