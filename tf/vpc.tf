resource "google_compute_network" "default" {
  project = var.project_id
  name    = "default"

  auto_create_subnetworks         = false
  routing_mode                    = "GLOBAL"
  bgp_best_path_selection_mode    = "LEGACY"
  delete_default_routes_on_create = false

  depends_on = [module.enabled_google_apis]
}

resource "google_compute_subnetwork" "default" {
  for_each = var.networks

  project                  = var.project_id
  name                     = google_compute_network.default.name
  ip_cidr_range            = each.value
  region                   = each.key
  network                  = google_compute_network.default.id
  private_ip_google_access = true
  stack_type               = "IPV4_ONLY"

  log_config {
    aggregation_interval = "INTERVAL_1_MIN"
    flow_sampling        = 0.01
    metadata             = "INCLUDE_ALL_METADATA"
    filter_expr          = "true"
  }
}

resource "google_compute_router" "default" {
  for_each = toset(keys(var.networks))

  project = var.project_id
  name    = "${each.key}-router"
  region  = each.key
  network = google_compute_network.default.id
}

resource "google_compute_router_nat" "default" {
  for_each = toset(keys(var.networks))

  project                            = var.project_id
  name                               = "${each.key}-nat"
  region                             = each.key
  router                             = google_compute_router.default[each.key].name
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"

  enable_dynamic_port_allocation = true
  min_ports_per_vm               = 1024
  max_ports_per_vm               = 16384

  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}
