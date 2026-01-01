package provider

import (
	"context"
	"fmt"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
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

var _ resource.Resource = &ServiceLimitsResource{}
var _ resource.ResourceWithImportState = &ServiceLimitsResource{}

func NewServiceLimitsResource() resource.Resource {
	return &ServiceLimitsResource{}
}

type ServiceLimitsResource struct {
	client *graphql.Client
}

type ServiceLimitsResourceModel struct {
	Id            types.String  `tfsdk:"id"`
	ServiceId     types.String  `tfsdk:"service_id"`
	EnvironmentId types.String  `tfsdk:"environment_id"`
	MemoryGB      types.Float64 `tfsdk:"memory_gb"`
	VCPUs         types.Float64 `tfsdk:"vcpus"`
}

func (r *ServiceLimitsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_limits"
}

func (r *ServiceLimitsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Railway service resource limits - configure CPU and memory allocation for a service instance.

This resource allows you to set resource limits (CPU and memory) for a specific service in a specific environment.
Resource limits help control costs and ensure services have the resources they need.

## Example Usage

` + "```hcl" + `
resource "railway_service_limits" "api_production" {
  service_id     = railway_service.api.id
  environment_id = railway_environment.production.id

  memory_gb = 2.0   # 2 GB of memory
  vcpus     = 1.0   # 1 vCPU
}

# Different limits for staging
resource "railway_service_limits" "api_staging" {
  service_id     = railway_service.api.id
  environment_id = railway_environment.staging.id

  memory_gb = 0.5   # 512 MB of memory
  vcpus     = 0.5   # 0.5 vCPU
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier (service_id:environment_id).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"service_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the service.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegex(), "must be a valid UUID"),
				},
			},
			"environment_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the environment.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegex(), "must be a valid UUID"),
				},
			},
			"memory_gb": schema.Float64Attribute{
				MarkdownDescription: "Memory allocation in GB (e.g., 0.5, 1, 2, 4, 8). Minimum is 0.25 GB.",
				Optional:            true,
				Validators: []validator.Float64{
					float64validator.AtLeast(0.25),
				},
			},
			"vcpus": schema.Float64Attribute{
				MarkdownDescription: "vCPU allocation (e.g., 0.5, 1, 2, 4, 8). Minimum is 0.25 vCPU.",
				Optional:            true,
				Validators: []validator.Float64{
					float64validator.AtLeast(0.25),
				},
			},
		},
	}
}

func (r *ServiceLimitsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ServiceLimitsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *ServiceLimitsResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Build the limits input
	input := r.buildLimitsInput(data)

	// Update the service instance limits
	_, err := updateServiceInstanceLimits(ctx, *r.client, input)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to set service limits, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "set service instance limits")

	// Set the composite ID
	data.Id = types.StringValue(fmt.Sprintf("%s:%s", data.ServiceId.ValueString(), data.EnvironmentId.ValueString()))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceLimitsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *ServiceLimitsResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Railway API doesn't provide a way to read limits back, so we preserve the configured state
	// The limits are write-only from Terraform's perspective

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceLimitsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *ServiceLimitsResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Build the limits input
	input := r.buildLimitsInput(data)

	// Update the service instance limits
	_, err := updateServiceInstanceLimits(ctx, *r.client, input)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update service limits, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated service instance limits")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceLimitsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Service limits cannot be deleted - they're properties of the service instance
	// When this resource is removed, the limits remain at their last configured values
	// To reset limits, the user would need to set them to default values before removing
	tflog.Trace(ctx, "service limits delete is a no-op - limits persist on the service")
}

func (r *ServiceLimitsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: service_id:environment_id
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ServiceLimitsResource) buildLimitsInput(data *ServiceLimitsResourceModel) ServiceInstanceLimitsUpdateInput {
	input := ServiceInstanceLimitsUpdateInput{
		ServiceId:     data.ServiceId.ValueString(),
		EnvironmentId: data.EnvironmentId.ValueString(),
	}

	if !data.MemoryGB.IsNull() {
		memoryGB := data.MemoryGB.ValueFloat64()
		input.MemoryGB = &memoryGB
	}

	if !data.VCPUs.IsNull() {
		vcpus := data.VCPUs.ValueFloat64()
		input.VCPUs = &vcpus
	}

	return input
}
