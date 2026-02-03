# only the default database supports the free tier
resource "google_firestore_database" "autoenergy" {
  project     = var.project_id
  name        = "(default)"
  location_id = "us-central1"
  type        = "FIRESTORE_NATIVE"

  depends_on = [module.enabled_google_apis]
}

resource "google_firestore_backup_schedule" "autoenergy_backup" {
  project  = var.project_id
  database = google_firestore_database.autoenergy.name

  retention = "259200s" # 3 days

  daily_recurrence {}
}

resource "google_project_iam_member" "autoenergy_firestore" {
  project = var.project_id
  role    = "roles/datastore.user"
  member  = "serviceAccount:${google_service_account.autoenergy.email}"
}
