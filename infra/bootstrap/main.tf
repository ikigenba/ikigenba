# The bucket predates the single-root layout and keeps its name: renaming a
# bucket is a destroy and recreate, and this one holds the state.
module "state_backend" {
  source      = "./modules/state-backend"
  bucket_name = "metaspot-dev-tfstate-295229566359"
}
