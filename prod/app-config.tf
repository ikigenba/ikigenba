# -----------------------------------------------------------------------------
# /metaspot/prod/app-config — generic, env-wide secrets/config blob.
#
# The "create a server" standard (see AGENTS.md). One hostname-independent
# parameter for the whole env. Terraform owns only its *existence* and shape;
# the real JSON content is populated and read out-of-band by services against
# this fixed path, so the value never enters Terraform state or plan output.
# Placeholder is valid empty JSON so a read-modify-write consumer can parse it
# before anything has been written.
# -----------------------------------------------------------------------------

resource "aws_ssm_parameter" "app_config" {
  name        = "/metaspot/prod/app-config"
  description = "Env-wide app config/secrets JSON blob. Content is script-managed out-of-band; Terraform owns only its existence."
  type        = "SecureString"
  value       = "{}"

  lifecycle {
    ignore_changes = [value]
  }
}
