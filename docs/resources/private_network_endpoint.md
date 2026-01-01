---
page_title: "railway_private_network_endpoint Resource - terraform-provider-railway"
subcategory: ""
description: |-
  Railway private network endpoint - connect services to private networks.
---

# railway_private_network_endpoint (Resource)

Railway private network endpoint - connect services to private networks.

Private network endpoints allow services to communicate over private networks,
enabling secure internal communication without public internet exposure.

## Example Usage

```hcl
resource "railway_project" "main" {
  name = "my-project"
}

resource "railway_environment" "production" {
  name       = "production"
  project_id = railway_project.main.id
}

resource "railway_service" "api" {
  name       = "api"
  project_id = railway_project.main.id
}

resource "railway_private_network" "internal" {
  name           = "internal"
  project_id     = railway_project.main.id
  environment_id = railway_environment.production.id
}

resource "railway_private_network_endpoint" "api" {
  private_network_id = railway_private_network.internal.id
  service_id         = railway_service.api.id
  environment_id     = railway_environment.production.id
  service_name       = "api"
  tags               = ["api", "backend"]
}

# Access the service via private DNS
# The API service will be accessible at: api.internal.railway.internal
```

## Argument Reference

The following arguments are supported:

* `private_network_id` - (Required) ID of the private network to connect to. Changing this forces a new resource to be created.
* `service_id` - (Required) ID of the service to connect to the private network. Must be a valid UUID. Changing this forces a new resource to be created.
* `environment_id` - (Required) Environment ID for the endpoint. Must be a valid UUID. Changing this forces a new resource to be created.
* `service_name` - (Required) Name for the service on the private network (used in DNS). Changing this forces a new resource to be created.
* `tags` - (Optional) List of tags for the endpoint.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

* `id` - Public identifier of the private network endpoint.
* `dns_name` - DNS name for accessing the service on the private network.
* `private_ips` - List of private IP addresses assigned to this endpoint.

## Import

Private network endpoints can be imported using the ID:

```shell
terraform import railway_private_network_endpoint.example <id>
```
