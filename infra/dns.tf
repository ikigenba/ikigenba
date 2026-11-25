# The root hosted zone. Delegated at the registrar from the management account,
# which is out of scope here. It holds no records of its own: the apex A
# record and every space's records are written by devctl.
resource "aws_route53_zone" "root" {
  name    = var.domain
  comment = "${var.domain} root; delegated at the registrar from the mgmt account"

  lifecycle {
    prevent_destroy = true
  }
}

moved {
  from = aws_route53_zone.env
  to   = aws_route53_zone.root
}

# The hosts zone. accounts on the hosts-operator space writes the A records of
# each <account>.<hosts domain> and each <host>.<hosts domain> and nothing
# else; every other record here is Terraform's. Registered in the mgmt
# account, which points the registration's name servers at this zone
# (hosts_zone_name_servers).
resource "aws_route53_zone" "hosts" {
  name    = local.hosts_domain
  comment = "${local.hosts_domain} SSH hosts; delegated at the registrar from the mgmt account"

  lifecycle {
    prevent_destroy = true
  }
}

# The records that moved with the zone from the mgmt account, unchanged. The
# apex and the two site names point at the `us` space's Elastic IP, which
# devctl owns and the space keeps for life; a literal, as in mgmt, since
# Terraform here never reads a space. accounts refuses any name that already
# has a record, so these names stay out of its reach.
locals {
  us_eip = "16.59.50.66"
}

resource "aws_route53_record" "hosts_apex_a" {
  zone_id = aws_route53_zone.hosts.zone_id
  name    = local.hosts_domain
  type    = "A"
  ttl     = 300
  records = [local.us_eip]
}

resource "aws_route53_record" "hosts_apex_mx" {
  zone_id = aws_route53_zone.hosts.zone_id
  name    = local.hosts_domain
  type    = "MX"
  ttl     = 60
  records = ["1 SMTP.GOOGLE.COM"]
}

resource "aws_route53_record" "hosts_apex_txt" {
  zone_id = aws_route53_zone.hosts.zone_id
  name    = local.hosts_domain
  type    = "TXT"
  ttl     = 60
  records = ["google-site-verification=SVxnpxWDWCdHfyExry-8qSdWZBuM1FUIJMsoRUjXrWQ"]
}

# Site names served by `sites` on `us`, until they are retired separately.
resource "aws_route53_record" "hosts_site" {
  for_each = toset(["dnd", "eastmark"])

  zone_id = aws_route53_zone.hosts.zone_id
  name    = "${each.key}.${local.hosts_domain}"
  type    = "A"
  ttl     = 300
  records = [local.us_eip]
}
