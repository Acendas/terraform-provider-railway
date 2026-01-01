package provider

import (
	"context"
	"fmt"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &PrivateNetworkResource{}
var _ resource.ResourceWithImportState = &PrivateNetworkResource{}

func NewPrivateNetworkResource() resource.Resource {
	return &PrivateNetworkResource{}
}

type PrivateNetworkResource struct {
	client *graphql.Client
}

type PrivateNetworkResourceModel struct {
	Id            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	ProjectId     types.String `tfsdk:"project_id"`
	EnvironmentId types.String `tfsdk:"environment_id"`
	DnsName       types.String `tfsdk:"dns_name"`
	Tags          types.List   `tfsdk:"tags"`
}

func (r *PrivateNetworkResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_private_network"
}

func (r *PrivateNetworkResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Railway private network - create secure internal networks for service communication.

Private networks allow services within the same environment to communicate securely without
going through the public internet. This reduces latency and egress costs.

## Example Usage

` + "```hcl" + `
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
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Public identifier of the private network.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the private network.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtLeast(1),
				},
			},
			"project_id": schema.StringAttribute{
				MarkdownDescription: "Project ID for the private network.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegex(), "must be a valid UUID"),
				},
			},
			"environment_id": schema.StringAttribute{
				MarkdownDescription: "Environment ID for the private network.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegex(), "must be a valid UUID"),
				},
			},
			"dns_name": schema.StringAttribute{
				MarkdownDescription: "DNS name for the private network.",
				Computed:            true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Tags for the private network.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *PrivateNetworkResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*graphql.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *graphql.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *PrivateNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *PrivateNetworkResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Build tags
	var tags []string
	if !data.Tags.IsNull() {
		resp.Diagnostics.Append(data.Tags.ElementsAs(ctx, &tags, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	} else {
		tags = []string{}
	}

	input := PrivateNetworkCreateOrGetInput{
		Name:          data.Name.ValueString(),
		ProjectId:     data.ProjectId.ValueString(),
		EnvironmentId: data.EnvironmentId.ValueString(),
		Tags:          tags,
	}

	response, err := createOrGetPrivateNetwork(ctx, *r.client, input)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create private network, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created private network")

	network := response.PrivateNetworkCreateOrGet

	data.Id = types.StringValue(network.PublicId)
	data.DnsName = types.StringValue(network.DnsName)

	// Update tags from response
	if len(network.Tags) > 0 {
		tagList, diags := types.ListValueFrom(ctx, types.StringType, network.Tags)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Tags = tagList
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PrivateNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *PrivateNetworkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Get the private networks for the environment and find ours
	response, err := getPrivateNetworks(ctx, *r.client, data.EnvironmentId.ValueString())

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read private networks, got error: %s", err))
		return
	}

	// Find our network by ID
	var found bool
	for _, network := range response.PrivateNetworks {
		if network.PublicId == data.Id.ValueString() {
			data.Name = types.StringValue(network.Name)
			data.DnsName = types.StringValue(network.DnsName)
			data.ProjectId = types.StringValue(network.ProjectId)
			data.EnvironmentId = types.StringValue(network.EnvironmentId)

			if len(network.Tags) > 0 {
				tagList, diags := types.ListValueFrom(ctx, types.StringType, network.Tags)
				resp.Diagnostics.Append(diags...)
				if resp.Diagnostics.HasError() {
					return
				}
				data.Tags = tagList
			}

			found = true
			break
		}
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PrivateNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Private networks are immutable - any changes require replacement
	// This is enforced by RequiresReplace on all configurable fields
	resp.Diagnostics.AddError(
		"Update Not Supported",
		"Private networks cannot be updated. Changes require replacement.",
	)
}

func (r *PrivateNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *PrivateNetworkResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Delete all private networks for the environment
	// Note: Railway API only provides deletion at environment level
	_, err := deletePrivateNetworksForEnvironment(ctx, *r.client, data.EnvironmentId.ValueString())

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete private network, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted private network")
}

func (r *PrivateNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
