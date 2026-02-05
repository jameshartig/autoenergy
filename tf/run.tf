resource "google_service_account" "autoenergy" {
  account_id = "autoenergy"

  depends_on = [module.enabled_google_apis]
}

resource "google_service_account" "autoenergy_build" {
  account_id = "autoenergy-build"

  depends_on = [module.enabled_google_apis]
}

resource "google_service_account_iam_member" "autoenergy_build_act_as" {
  service_account_id = google_service_account.autoenergy.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.autoenergy_build.email}"
}

resource "google_project_iam_member" "autoenergy_logs_writer" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.autoenergy.email}"
}

resource "google_project_iam_member" "autoenergy_build_logs_writer" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.autoenergy_build.email}"
}

resource "google_artifact_registry_repository" "autoenergy" {
  project       = var.project_id
  location      = "us-central1"
  repository_id = "autoenergy"
  format        = "DOCKER"
}

resource "google_artifact_registry_repository_iam_member" "autoenergy_build" {
  project    = google_artifact_registry_repository.autoenergy.project
  location   = google_artifact_registry_repository.autoenergy.location
  repository = google_artifact_registry_repository.autoenergy.name
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.autoenergy_build.email}"
}

resource "google_cloudbuild_trigger" "github" {
  project         = var.project_id
  location        = "us-central1"
  service_account = google_service_account.autoenergy_build.id

  repository_event_config {
    repository = "projects/${var.project_id}/locations/us-central1/connections/github-jameshartig/repositories/jameshartig-autoenergy"
    push {
      branch = "^(main|wip)$"
    }
  }

  build {
    images = [
      "${google_artifact_registry_repository.autoenergy.registry_uri}/autoenergy:$COMMIT_SHA",
      "${google_artifact_registry_repository.autoenergy.registry_uri}/autoenergy:latest",
    ]

    step {
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t",
        "${google_artifact_registry_repository.autoenergy.registry_uri}/autoenergy:$COMMIT_SHA",
        "-t",
        "${google_artifact_registry_repository.autoenergy.registry_uri}/autoenergy:latest",
        ".",
      ]
    }

    step {
      name = "gcr.io/cloud-builders/docker"
      args = [
        "push",
        "${google_artifact_registry_repository.autoenergy.registry_uri}/autoenergy:$COMMIT_SHA",
      ]
    }

    step {
      name       = "gcr.io/google.com/cloudsdktool/cloud-sdk:slim"
      entrypoint = "gcloud"
      args = [
        "run",
        "services",
        "update",
        "autoenergy",
        "--platform=managed",
        "--image=${google_artifact_registry_repository.autoenergy.registry_uri}/autoenergy:$COMMIT_SHA",
        "--region=us-central1",
        "--quiet",
      ]
    }

    options {
      logging = "CLOUD_LOGGING_ONLY"
    }
  }
}

locals {
  # from https://docs.cloud.google.com/run/docs/triggering/https-request#deterministic
  # we can't use the uri from the run resource because it's not known at plan time
  run_deterministic_uri = "https://autoenergy-${data.google_project.autoenergy.number}.us-central1.run.app"
}

resource "google_cloud_run_v2_service" "autoenergy" {
  provider             = google-beta
  project              = var.project_id
  name                 = "autoenergy"
  location             = "us-central1"
  ingress              = "INGRESS_TRAFFIC_ALL"
  default_uri_disabled = false

  scaling {
    min_instance_count = 0
    max_instance_count = 1
  }

  template {
    max_instance_request_concurrency = 1000
    service_account                  = google_service_account.autoenergy.email
    timeout                          = "60s"

    containers {
      image = "us-docker.pkg.dev/cloudrun/container/hello"

      resources {
        limits = {
          cpu    = "1"
          memory = "128Mi"
        }
        cpu_idle          = true
        startup_cpu_boost = false
      }

      liveness_probe {
        timeout_seconds = 1
        period_seconds  = 15
        http_get {
          path = "/healthz"
        }
      }

      startup_probe {
        timeout_seconds = 1
        period_seconds  = 1
        http_get {
          path = "/healthz"
        }
      }

      env {
        name  = "LOG_LEVEL"
        value = "debug"
      }

      env {
        name  = "UPDATE_SPECIFIC_AUDIENCE"
        value = local.run_deterministic_uri
      }

      env {
        name  = "UPDATE_SPECIFIC_EMAIL"
        value = google_service_account.autoenergy.email
      }

      env {
        name  = "CONFIG_JSON_FILE"
        value = "/secrets/config.json"
      }

      volume_mounts {
        name       = "secrets"
        mount_path = "/secrets"
      }
    }

    volumes {
      name = "secrets"
      secret {
        secret = "autoenergy-secrets"
        items {
          version = "latest"
          path    = "config.json"
        }
      }
    }

    vpc_access {
      network_interfaces {
        network = google_compute_network.default.id
      }
    }
  }

  lifecycle {
    ignore_changes = [
      client,
      client_version,
      template[0].containers[0].image
    ]
  }

  depends_on = [google_cloudbuild_trigger.github]
}

resource "google_cloud_run_v2_service_iam_member" "autoenergy_build" {
  project  = var.project_id
  location = google_cloud_run_v2_service.autoenergy.location
  name     = google_cloud_run_v2_service.autoenergy.name
  role     = "roles/run.developer"
  member   = "serviceAccount:${google_service_account.autoenergy_build.email}"
}

resource "google_cloud_run_v2_service_iam_binding" "autoenergy" {
  project  = var.project_id
  location = google_cloud_run_v2_service.autoenergy.location
  name     = google_cloud_run_v2_service.autoenergy.name
  role     = "roles/run.invoker"
  members = [
    "allUsers"
  ]
}
