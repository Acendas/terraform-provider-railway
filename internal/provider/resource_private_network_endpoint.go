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

var _ resource.Resource = &PrivateNetworkEndpointResource{}
var _ resource.ResourceWithImportState = &PrivateNetworkEndpointResource{}

func NewPrivateNetworkEndpointResource() resource.Resource {
	return &PrivateNetworkEndpointResource{}
}

type PrivateNetworkEndpointResource struct {
	client *graphql.Client
}

type PrivateNetworkEndpointResourceModel struct {
	Id               types.String `tfsdk:"id"`
	PrivateNetworkId types.String `tfsdk:"private_network_id"`
	ServiceId        types.String `tfsdk:"service_id"`
	EnvironmentId    types.String `tfsdk:"environment_id"`
	ServiceName      types.String `tfsdk:"service_name"`
	DnsName          types.String `tfsdk:"dns_name"`
	PrivateIps       types.List   `tfsdk:"private_ips"`
	Tags             types.List   `tfsdk:"tags"`
}

func (r *PrivateNetworkEndpointResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_private_network_endpoint"
}

func (r *PrivateNetworkEndpointResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Railway private network endpoint - connect services to private networks.

Private network endpoints allow services to communicate over private networks,
enabling secure internal communication without public internet exposure.

## Example Usage

` + "```hcl" + `
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
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Public identifier of the private network endpoint.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"private_network_id": schema.StringAttribute{
				MarkdownDescription: "ID of the private network to connect to.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtLeast(1),
				},
			},
			"service_id": schema.StringAttribute{
				MarkdownDescription: "ID of the service to connect to the private network.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegex(), "must be a valid UUID"),
				},
			},
			"environment_id": schema.StringAttribute{
				MarkdownDescription: "Environment ID for the endpoint.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegex(), "must be a valid UUID"),
				},
			},
			"service_name": schema.StringAttribute{
				MarkdownDescription: "Name for the service on the private network (used in DNS).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtLeast(1),
				},
			},
			"dns_name": schema.StringAttribute{
				MarkdownDescription: "DNS name for accessing the service on the private network.",
				Computed:            true,
			},
			"private_ips": schema.ListAttribute{
				MarkdownDescription: "Private IP addresses assigned to this endpoint.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Tags for the endpoint.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *PrivateNetworkEndpointResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *PrivateNetworkEndpointResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *PrivateNetworkEndpointResourceModel

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

	input := PrivateNetworkEndpointCreateOrGetInput{
		PrivateNetworkId: data.PrivateNetworkId.ValueString(),
		ServiceId:        data.ServiceId.ValueString(),
		EnvironmentId:    data.EnvironmentId.ValueString(),
		ServiceName:      data.ServiceName.ValueString(),
		Tags:             tags,
	}

	response, err := createOrGetPrivateNetworkEndpoint(ctx, *r.client, input)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create private network endpoint, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created private network endpoint")

	endpoint := response.PrivateNetworkEndpointCreateOrGet

	data.Id = types.StringValue(endpoint.PublicId)
	data.DnsName = types.StringValue(endpoint.DnsName)

	// Update private IPs from response
	if len(endpoint.PrivateIps) > 0 {
		ipList, diags := types.ListValueFrom(ctx, types.StringType, endpoint.PrivateIps)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.PrivateIps = ipList
	} else {
		data.PrivateIps = types.ListNull(types.StringType)
	}

	// Update tags from response
	if len(endpoint.Tags) > 0 {
		tagList, diags := types.ListValueFrom(ctx, types.StringType, endpoint.Tags)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Tags = tagList
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PrivateNetworkEndpointResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *PrivateNetworkEndpointResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Get the endpoint - pass pointers to the function
	envId := data.EnvironmentId.ValueString()
	networkId := data.PrivateNetworkId.ValueString()
	serviceId := data.ServiceId.ValueString()

	response, err := getPrivateNetworkEndpoint(ctx, *r.client,
		&envId,
		&networkId,
		&serviceId,
	)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read private network endpoint, got error: %s", err))
		return
	}

	// Check if endpoint exists
	if response.PrivateNetworkEndpoint == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	endpoint := response.PrivateNetworkEndpoint

	// Handle pointer fields from response
	if endpoint.PublicId != nil {
		data.Id = types.StringValue(*endpoint.PublicId)
	}
	if endpoint.DnsName != nil {
		data.DnsName = types.StringValue(*endpoint.DnsName)
	}

	// Update private IPs - convert []*string to []string
	if len(endpoint.PrivateIps) > 0 {
		ips := make([]string, 0, len(endpoint.PrivateIps))
		for _, ip := range endpoint.PrivateIps {
			if ip != nil {
				ips = append(ips, *ip)
			}
		}
		ipList, diags := types.ListValueFrom(ctx, types.StringType, ips)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.PrivateIps = ipList
	} else {
		data.PrivateIps = types.ListNull(types.StringType)
	}

	// Update tags - convert []*string to []string
	if len(endpoint.Tags) > 0 {
		tags := make([]string, 0, len(endpoint.Tags))
		for _, tag := range endpoint.Tags {
			if tag != nil {
				tags = append(tags, *tag)
			}
		}
		tagList, diags := types.ListValueFrom(ctx, types.StringType, tags)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Tags = tagList
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PrivateNetworkEndpointResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Endpoints are immutable - any changes require replacement
	// This is enforced by RequiresReplace on all configurable fields
	resp.Diagnostics.AddError(
		"Update Not Supported",
		"Private network endpoints cannot be updated. Changes require replacement.",
	)
}

func (r *PrivateNetworkEndpointResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *PrivateNetworkEndpointResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	_, err := deletePrivateNetworkEndpoint(ctx, *r.client, data.Id.ValueString())

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete private network endpoint, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted private network endpoint")
}

func (r *PrivateNetworkEndpointResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
