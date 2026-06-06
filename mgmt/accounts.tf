locals {
  member_accounts = {
    prod = {
      name  = "Production"
      email = "mgreenly+aws.prod@gmail.com"
      admin = true
    }
    test = {
      name  = "Test"
      email = "mgreenly+aws.test@gmail.com"
      admin = true
    }
    sandbox = {
      name  = "Sandbox"
      email = "mgreenly+aws.sandbox@gmail.com"
      admin = true
    }
    int = {
      name  = "int"
      email = "mgreenly+int@gmail.com"
      admin = true
    }
    example1_test = {
      name  = "example1"
      email = "mgreenly+test.example1@gmail.com"
      admin = false
    }
    example2_test = {
      name  = "example2"
      email = "mgreenly+test.example2@gmail.com"
      admin = false
    }
    example1_prod = {
      name  = "example1"
      email = "mgreenly+prod.example1@gmail.com"
      admin = false
    }
  }
}

data "aws_ssoadmin_instances" "this" {}

data "aws_identitystore_group" "administrators" {
  identity_store_id = tolist(data.aws_ssoadmin_instances.this.identity_store_ids)[0]

  alternate_identifier {
    unique_attribute {
      attribute_path  = "DisplayName"
      attribute_value = "Administrators"
    }
  }
}

data "aws_ssoadmin_permission_set" "admin" {
  instance_arn = tolist(data.aws_ssoadmin_instances.this.arns)[0]
  name         = "AdministratorAccess"
}

resource "aws_organizations_account" "this" {
  for_each = local.member_accounts

  name  = each.value.name
  email = each.value.email

  iam_user_access_to_billing = "ALLOW"
  role_name                  = "OrganizationAccountAccessRole"

  lifecycle {
    ignore_changes = [iam_user_access_to_billing, role_name]
  }
}

resource "aws_ssoadmin_account_assignment" "admin" {
  for_each = { for k, v in local.member_accounts : k => v if v.admin }

  instance_arn       = tolist(data.aws_ssoadmin_instances.this.arns)[0]
  permission_set_arn = data.aws_ssoadmin_permission_set.admin.arn

  principal_type = "GROUP"
  principal_id   = data.aws_identitystore_group.administrators.group_id

  target_id   = aws_organizations_account.this[each.key].id
  target_type = "AWS_ACCOUNT"
}
