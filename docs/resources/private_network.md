---
page_title: "railway_private_network Resource - terraform-provider-railway"
subcategory: ""
description: |-
  Railway private network - create secure internal networks for service communication.
---

# railway_private_network (Resource)

Railway private network - create secure internal networks for service communication.

Private networks allow services within the same environment to communicate securely without
going through the public internet. This reduces latency and egress costs.

## Example Usage

```hcl
resource "railway_project" "main" {
  name = "my-project"
}

resource "railway_environment" "production" {
  name       = "production"
  project_id = railway_project.main.id
}

resource "railway_private_network" "internal" {
  name           = "internal"
  project_id     = railway_project.main.id
  environment_id = railway_environment.production.id
  tags           = ["production", "internal"]
}

# Connect a service to the private network
resource "railway_private_network_endpoint" "api" {
  private_network_id = railway_private_network.internal.id
  service_id         = railway_service.api.id
  environment_id     = railway_environment.production.id
  service_name       = "api"
}
```

## Argument Reference

The following arguments are supported:

* `name` - (Required) Name of the private network. Changing this forces a new resource to be created.
* `project_id` - (Required) Project ID for the private network. Must be a valid UUID. Changing this forces a new resource to be created.
* `environment_id` - (Required) Environment ID for the private network. Must be a valid UUID. Changing this forces a new resource to be created.
* `tags` - (Optional) List of tags for the private network.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

* `id` - Public identifier of the private network.
* `dns_name` - DNS name for the private network.

## Import

Private networks can be imported using the ID:

```shell
terraform import railway_private_network.example <id>
```
