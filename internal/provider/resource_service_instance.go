package provider

import (
	"context"
	"fmt"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &ServiceInstanceResource{}
var _ resource.ResourceWithImportState = &ServiceInstanceResource{}

func NewServiceInstanceResource() resource.Resource {
	return &ServiceInstanceResource{}
}

type ServiceInstanceResource struct {
	client *graphql.Client
}

type ServiceInstanceResourceModel struct {
	Id                       types.String `tfsdk:"id"`
	ServiceId                types.String `tfsdk:"service_id"`
	EnvironmentId            types.String `tfsdk:"environment_id"`
	SourceImage              types.String `tfsdk:"source_image"`
	SourceRepo               types.String `tfsdk:"source_repo"`
	RegistryCredentialsUser  types.String `tfsdk:"registry_credentials_username"`
	RegistryCredentialsPass  types.String `tfsdk:"registry_credentials_password"`
	Redeploy                 types.Bool   `tfsdk:"redeploy"`
}

func (r *ServiceInstanceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_instance"
}

func (r *ServiceInstanceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Railway service instance - environment-scoped service configuration.

This resource allows you to configure a service instance for a specific environment,
including setting the source image independently per environment.

Unlike ` + "`railway_service`" + ` which sets the source image at the project level (affecting all environments),
this resource lets you deploy different images to staging vs production environments.

## Example Usage

` + "```hcl" + `
# Create the service (project-scoped)
resource "railway_service" "api" {
  name       = "api"
  project_id = railway_project.main.id
}

# Configure staging instance with staging image
resource "railway_service_instance" "api_staging" {
  service_id     = railway_service.api.id
  environment_id = railway_environment.staging.id
  source_image   = "ghcr.io/myorg/api:staging"

  registry_credentials_username = var.ghcr_username
  registry_credentials_password = var.ghcr_token
}

# Configure production instance with production image
resource "railway_service_instance" "api_production" {
  service_id     = railway_service.api.id
  environment_id = railway_environment.production.id
  source_image   = "ghcr.io/myorg/api:v1.2.3"

  registry_credentials_username = var.ghcr_username
  registry_credentials_password = var.ghcr_token
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier of the service instance (service_id:environment_id).",
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
			"source_image": schema.StringAttribute{
				MarkdownDescription: "Docker image to deploy for this service instance. Conflicts with `source_repo`.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtLeast(1),
					stringvalidator.ConflictsWith(path.MatchRoot("source_repo")),
				},
			},
			"source_repo": schema.StringAttribute{
				MarkdownDescription: "GitHub repository to deploy for this service instance. Conflicts with `source_image`.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtLeast(1),
					stringvalidator.ConflictsWith(path.MatchRoot("source_image")),
				},
			},
			"registry_credentials_username": schema.StringAttribute{
				MarkdownDescription: "Username for private Docker registry authentication.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtLeast(1),
					stringvalidator.AlsoRequires(path.MatchRoot("registry_credentials_password")),
				},
			},
			"registry_credentials_password": schema.StringAttribute{
				MarkdownDescription: "Password for private Docker registry authentication.",
				Optional:            true,
				Sensitive:           true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthAtLeast(1),
					stringvalidator.AlsoRequires(path.MatchRoot("registry_credentials_username")),
				},
			},
			"redeploy": schema.BoolAttribute{
				MarkdownDescription: "Whether to trigger a redeployment after updating the service instance. **Default** `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
		},
	}
}

func (r *ServiceInstanceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ServiceInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *ServiceInstanceResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Build the update input
	input := r.buildUpdateInput(data)

	// Update the service instance
	_, err := updateServiceInstanceWithEnv(
		ctx,
		*r.client,
		data.EnvironmentId.ValueString(),
		data.ServiceId.ValueString(),
		input,
	)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update service instance, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated service instance")

	// Trigger redeployment if enabled
	if data.Redeploy.ValueBool() {
		_, err = redeployServiceInstanceWithEnv(
			ctx,
			*r.client,
			data.EnvironmentId.ValueString(),
			data.ServiceId.ValueString(),
		)

		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to redeploy service instance, got error: %s", err))
			return
		}

		tflog.Trace(ctx, "redeployed service instance")
	}

	// Set the composite ID
	data.Id = types.StringValue(fmt.Sprintf("%s:%s", data.ServiceId.ValueString(), data.EnvironmentId.ValueString()))

	// Read back the current state
	err = r.readServiceInstance(ctx, data)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read service instance, got error: %s", err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *ServiceInstanceResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.readServiceInstance(ctx, data)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read service instance, got error: %s", err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *ServiceInstanceResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Build the update input
	input := r.buildUpdateInput(data)

	// Update the service instance
	_, err := updateServiceInstanceWithEnv(
		ctx,
		*r.client,
		data.EnvironmentId.ValueString(),
		data.ServiceId.ValueString(),
		input,
	)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update service instance, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated service instance")

	// Trigger redeployment if enabled
	if data.Redeploy.ValueBool() {
		_, err = redeployServiceInstanceWithEnv(
			ctx,
			*r.client,
			data.EnvironmentId.ValueString(),
			data.ServiceId.ValueString(),
		)

		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to redeploy service instance, got error: %s", err))
			return
		}

		tflog.Trace(ctx, "redeployed service instance")
	}

	// Read back the current state
	err = r.readServiceInstance(ctx, data)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read service instance, got error: %s", err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServiceInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Service instances cannot be deleted - they exist as long as the service exists
	// This is a no-op; the service instance will be cleaned up when the service is deleted
	tflog.Trace(ctx, "service instance delete is a no-op - instances are managed by the parent service")
}

func (r *ServiceInstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: service_id:environment_id
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *ServiceInstanceResource) buildUpdateInput(data *ServiceInstanceResourceModel) ServiceInstanceUpdateInput {
	var input ServiceInstanceUpdateInput

	// Set source (image or repo)
	if !data.SourceImage.IsNull() || !data.SourceRepo.IsNull() {
		source := &ServiceSourceInput{}

		if !data.SourceImage.IsNull() {
			source.Image = data.SourceImage.ValueStringPointer()
		}

		if !data.SourceRepo.IsNull() {
			source.Repo = data.SourceRepo.ValueStringPointer()
		}

		input.Source = source
	}

	// Set registry credentials
	if !data.RegistryCredentialsUser.IsNull() && !data.RegistryCredentialsPass.IsNull() {
		input.RegistryCredentials = &RegistryCredentialsInput{
			Username: data.RegistryCredentialsUser.ValueString(),
			Password: data.RegistryCredentialsPass.ValueString(),
		}
	}

	return input
}

func (r *ServiceInstanceResource) readServiceInstance(ctx context.Context, data *ServiceInstanceResourceModel) error {
	response, err := getServiceInstanceForResource(
		ctx,
		*r.client,
		data.EnvironmentId.ValueString(),
		data.ServiceId.ValueString(),
	)

	if err != nil {
		return err
	}

	// Update data from response
	if response.ServiceInstance.Source != nil {
		if response.ServiceInstance.Source.Image != nil {
			data.SourceImage = types.StringValue(*response.ServiceInstance.Source.Image)
		} else {
			data.SourceImage = types.StringNull()
		}

		if response.ServiceInstance.Source.Repo != nil {
			data.SourceRepo = types.StringValue(*response.ServiceInstance.Source.Repo)
		} else {
			data.SourceRepo = types.StringNull()
		}
	}

	return nil
}
