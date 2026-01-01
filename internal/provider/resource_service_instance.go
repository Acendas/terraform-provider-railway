package provider

import (
	"context"
	"fmt"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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

	// Build configuration
	Builder          types.String `tfsdk:"builder"`
	BuildCommand     types.String `tfsdk:"build_command"`
	StartCommand     types.String `tfsdk:"start_command"`
	PreDeployCommand types.List   `tfsdk:"pre_deploy_command"`

	// Health checks
	HealthcheckPath    types.String `tfsdk:"healthcheck_path"`
	HealthcheckTimeout types.Int64  `tfsdk:"healthcheck_timeout"`

	// Restart policies
	RestartPolicyType       types.String `tfsdk:"restart_policy_type"`
	RestartPolicyMaxRetries types.Int64  `tfsdk:"restart_policy_max_retries"`

	// Serverless mode
	SleepApplication types.Bool `tfsdk:"sleep_application"`
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

  # Enable serverless mode for staging (sleeps when inactive)
  sleep_application = true

  # Health check configuration
  healthcheck_path    = "/health"
  healthcheck_timeout = 30
}

# Configure production instance with production image
resource "railway_service_instance" "api_production" {
  service_id     = railway_service.api.id
  environment_id = railway_environment.production.id
  source_image   = "ghcr.io/myorg/api:v1.2.3"

  registry_credentials_username = var.ghcr_username
  registry_credentials_password = var.ghcr_token

  # Production should always restart on failure
  restart_policy_type       = "ON_FAILURE"
  restart_policy_max_retries = 3

  # Custom start command
  start_command = "npm run start:prod"

  # Run database migrations before deployment
  pre_deploy_command = ["npm run db:migrate"]

  # Health check configuration
  healthcheck_path    = "/health"
  healthcheck_timeout = 60
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

			// Build configuration
			"builder": schema.StringAttribute{
				MarkdownDescription: "Build system to use. Valid values: `NIXPACKS`, `HEROKU`, `PAKETO`, `RAILPACK`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("NIXPACKS", "HEROKU", "PAKETO", "RAILPACK"),
				},
			},
			"build_command": schema.StringAttribute{
				MarkdownDescription: "Custom build command to run during the build phase.",
				Optional:            true,
			},
			"start_command": schema.StringAttribute{
				MarkdownDescription: "Custom start command to run the application.",
				Optional:            true,
			},
			"pre_deploy_command": schema.ListAttribute{
				MarkdownDescription: "Commands to run before deployment (e.g., database migrations).",
				Optional:            true,
				ElementType:         types.StringType,
			},

			// Health checks
			"healthcheck_path": schema.StringAttribute{
				MarkdownDescription: "HTTP path for health checks (e.g., `/health`). Railway will poll this endpoint to determine service health.",
				Optional:            true,
			},
			"healthcheck_timeout": schema.Int64Attribute{
				MarkdownDescription: "Timeout in seconds for health check requests.",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},

			// Restart policies
			"restart_policy_type": schema.StringAttribute{
				MarkdownDescription: "Restart policy type. Valid values: `ALWAYS`, `NEVER`, `ON_FAILURE`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("ALWAYS", "NEVER", "ON_FAILURE"),
				},
			},
			"restart_policy_max_retries": schema.Int64Attribute{
				MarkdownDescription: "Maximum number of restart retries when using `ON_FAILURE` policy.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},

			// Serverless mode
			"sleep_application": schema.BoolAttribute{
				MarkdownDescription: "Enable serverless mode. When enabled, the application sleeps after 10 minutes of inactivity and wakes on incoming requests.",
				Optional:            true,
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
	input := r.buildUpdateInput(ctx, data)

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
	input := r.buildUpdateInput(ctx, data)

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

func (r *ServiceInstanceResource) buildUpdateInput(ctx context.Context, data *ServiceInstanceResourceModel) ServiceInstanceUpdateInput {
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

	// Build configuration
	if !data.Builder.IsNull() {
		builder := Builder(data.Builder.ValueString())
		input.Builder = &builder
	}

	if !data.BuildCommand.IsNull() {
		input.BuildCommand = data.BuildCommand.ValueStringPointer()
	}

	if !data.StartCommand.IsNull() {
		input.StartCommand = data.StartCommand.ValueStringPointer()
	}

	if !data.PreDeployCommand.IsNull() {
		var cmds []string
		data.PreDeployCommand.ElementsAs(ctx, &cmds, false)
		// Convert []string to []*string
		ptrCmds := make([]*string, len(cmds))
		for i := range cmds {
			ptrCmds[i] = &cmds[i]
		}
		input.PreDeployCommand = ptrCmds
	}

	// Health checks
	if !data.HealthcheckPath.IsNull() {
		input.HealthcheckPath = data.HealthcheckPath.ValueStringPointer()
	}

	if !data.HealthcheckTimeout.IsNull() {
		timeout := int(data.HealthcheckTimeout.ValueInt64())
		input.HealthcheckTimeout = &timeout
	}

	// Restart policies
	if !data.RestartPolicyType.IsNull() {
		policyType := RestartPolicyType(data.RestartPolicyType.ValueString())
		input.RestartPolicyType = &policyType
	}

	if !data.RestartPolicyMaxRetries.IsNull() {
		retries := int(data.RestartPolicyMaxRetries.ValueInt64())
		input.RestartPolicyMaxRetries = &retries
	}

	// Serverless mode
	if !data.SleepApplication.IsNull() {
		input.SleepApplication = data.SleepApplication.ValueBoolPointer()
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

	instance := response.ServiceInstance

	// Update source from response
	if instance.Source != nil {
		if instance.Source.Image != nil {
			data.SourceImage = types.StringValue(*instance.Source.Image)
		} else {
			data.SourceImage = types.StringNull()
		}

		if instance.Source.Repo != nil {
			data.SourceRepo = types.StringValue(*instance.Source.Repo)
		} else {
			data.SourceRepo = types.StringNull()
		}
	}

	// Build configuration
	data.Builder = types.StringValue(string(instance.Builder))

	if instance.BuildCommand != nil {
		data.BuildCommand = types.StringValue(*instance.BuildCommand)
	} else {
		data.BuildCommand = types.StringNull()
	}

	if instance.StartCommand != nil {
		data.StartCommand = types.StringValue(*instance.StartCommand)
	} else {
		data.StartCommand = types.StringNull()
	}

	// PreDeployCommand: Railway API returns this as JSON type which genqlient decodes as map[string]interface{}
	// Since the format is inconsistent, we preserve the user's configured value rather than reading from API
	// The value is write-only from Terraform's perspective
	if data.PreDeployCommand.IsUnknown() {
		data.PreDeployCommand = types.ListNull(types.StringType)
	}

	// Health checks
	if instance.HealthcheckPath != nil {
		data.HealthcheckPath = types.StringValue(*instance.HealthcheckPath)
	} else {
		data.HealthcheckPath = types.StringNull()
	}

	if instance.HealthcheckTimeout != nil {
		data.HealthcheckTimeout = types.Int64Value(int64(*instance.HealthcheckTimeout))
	} else {
		data.HealthcheckTimeout = types.Int64Null()
	}

	// Restart policies
	data.RestartPolicyType = types.StringValue(string(instance.RestartPolicyType))
	data.RestartPolicyMaxRetries = types.Int64Value(int64(instance.RestartPolicyMaxRetries))

	// Serverless mode
	if instance.SleepApplication != nil {
		data.SleepApplication = types.BoolValue(*instance.SleepApplication)
	} else {
		data.SleepApplication = types.BoolNull()
	}

	return nil
}
