resource "google_cloud_scheduler_job" "autoenergy_update" {
  name        = "autoenergy-update"
  description = "Triggers the /api/update endpoint every 15 minutes"
  schedule    = "*/15 * * * *"
  time_zone   = "America/Chicago"
  region      = "us-central1"
  project     = var.project_id
  paused      = !var.schedule_enabled

  http_target {
    http_method = "POST"
    uri         = "${local.run_deterministic_uri}/api/update"

    oidc_token {
      service_account_email = google_service_account.autoenergy.email
      audience              = local.run_deterministic_uri
    }
  }
}
