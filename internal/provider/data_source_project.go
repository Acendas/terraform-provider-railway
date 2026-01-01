package provider

import (
	"context"
	"fmt"

	"github.com/Khan/genqlient/graphql"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &ProjectDataSource{}

func NewProjectDataSource() datasource.DataSource {
	return &ProjectDataSource{}
}

type ProjectDataSource struct {
	client *graphql.Client
}

type ProjectDataSourceModel struct {
	Id                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	IsPublic           types.Bool   `tfsdk:"is_public"`
	HasPrDeploys       types.Bool   `tfsdk:"has_pr_deploys"`
	WorkspaceId        types.String `tfsdk:"workspace_id"`
	DefaultEnvironment types.String `tfsdk:"default_environment_id"`
}

func (d *ProjectDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *ProjectDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Look up an existing Railway project by ID.

## Example Usage

` + "```hcl" + `
data "railway_project" "existing" {
  id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}

output "project_name" {
  value = data.railway_project.existing.name
}
` + "```" + `
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Project identifier.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegex(), "must be a valid UUID"),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Project name.",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Project description.",
				Computed:            true,
			},
			"is_public": schema.BoolAttribute{
				MarkdownDescription: "Whether the project is public.",
				Computed:            true,
			},
			"has_pr_deploys": schema.BoolAttribute{
				MarkdownDescription: "Whether PR deploys are enabled.",
				Computed:            true,
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "Workspace ID the project belongs to.",
				Computed:            true,
			},
			"default_environment_id": schema.StringAttribute{
				MarkdownDescription: "ID of the default (oldest) environment in the project.",
				Computed:            true,
			},
		},
	}
}

func (d *ProjectDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*graphql.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *graphql.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = client
}

func (d *ProjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ProjectDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	response, err := getProject(ctx, *d.client, data.Id.ValueString())

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read project, got error: %s", err))
		return
	}

	project := response.Project.Project

	data.Name = types.StringValue(project.Name)
	data.Description = types.StringValue(project.Description)
	data.IsPublic = types.BoolValue(project.IsPublic)
	data.HasPrDeploys = types.BoolValue(project.PrDeploys)

	if project.Workspace != nil {
		data.WorkspaceId = types.StringValue(project.Workspace.Id)
	} else {
		data.WorkspaceId = types.StringNull()
	}

	// Find the default (oldest) environment
	if len(project.Environments.Edges) > 0 {
		// The edges are sorted by createdAt, so the first one is the oldest
		data.DefaultEnvironment = types.StringValue(project.Environments.Edges[0].Node.Id)
	} else {
		data.DefaultEnvironment = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
