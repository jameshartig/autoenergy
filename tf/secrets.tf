resource "google_secret_manager_secret" "autoenergy_secrets" {
  project   = var.project_id
  secret_id = "autoenergy-secrets"

  replication {
    auto {}
  }

  depends_on = [module.enabled_google_apis]
}

resource "google_secret_manager_secret_iam_member" "autoenergy_accessor" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.autoenergy_secrets.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.autoenergy.email}"
}
