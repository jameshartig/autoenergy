resource "google_cloud_scheduler_job" "autoenergy_update" {
  name        = "autoenergy-update"
  description = "Triggers the /api/update endpoint at minutes 13, 33, 53"
  # This runs at 13, 33, 53 minutes past the hour to give us enough time to
  # have 2 datapoints from the current hour at least.
  # we used to run every 15 minutes but the 0th minute of the hour didn't have
  # data yet for that hour
  schedule  = "13,33,53 * * * *"
  time_zone = "America/Chicago"
  region    = "us-central1"
  project   = var.project_id
  paused    = !var.schedule_enabled
  # this just needs to be larger than the run service timeout
  attempt_deadline = "90s"

  http_target {
    http_method = "POST"
    uri         = "${local.run_deterministic_uri}/api/update"

    oidc_token {
      service_account_email = google_service_account.autoenergy.email
      audience              = local.run_deterministic_uri
    }
  }
}
