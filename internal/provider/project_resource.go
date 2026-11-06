package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

var (
	_ resource.Resource                = (*projectResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectResource)(nil)
	_ resource.ResourceWithImportState = (*projectResource)(nil)
)

// NewProjectResource returns the paperclip_project resource.
func NewProjectResource() resource.Resource {
	return &projectResource{}
}

type projectResource struct {
	client *client.Client
}

type projectResourceModel struct {
	ID          types.String `tfsdk:"id"`
	CompanyID   types.String `tfsdk:"company_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Status      types.String `tfsdk:"status"`
	GoalIDs     types.Set    `tfsdk:"goal_ids"`
	LeadAgentID types.String `tfsdk:"lead_agent_id"`
	TargetDate  types.String `tfsdk:"target_date"`
	Color       types.String `tfsdk:"color"`
	URLKey      types.String `tfsdk:"url_key"`
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Paperclip project, a body of work that groups tasks and links them to goals.\n\n" +
			"Workspaces, repositories and access members of a project are not managed by this resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the project.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"company_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the company the project belongs to. Changing this replaces the project.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the project.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "What the project is about.",
			},
			"status": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Status of the project: `backlog`, `planned`, `in_progress`, `completed` or " +
					"`cancelled`. Defaults to `backlog`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"goal_ids": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Default:             setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
				MarkdownDescription: "Identifiers of the goals the project contributes to.",
			},
			"lead_agent_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Identifier of the agent that leads the project.",
			},
			"target_date": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Target completion date, as `YYYY-MM-DD`.",
			},
			"color": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Colour of the project in the UI, as a hex code such as `#6366f1`. Chosen by Paperclip when omitted.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL-safe key of the project, derived from its name.",
			},
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (m *projectResourceModel) write(ctx context.Context, diags *diag.Diagnostics) client.ProjectWrite {
	goalIDs := []string{}
	if !m.GoalIDs.IsNull() && !m.GoalIDs.IsUnknown() {
		diags.Append(m.GoalIDs.ElementsAs(ctx, &goalIDs, false)...)
	}
	return client.ProjectWrite{
		Name:        m.Name.ValueString(),
		Description: stringPointer(m.Description),
		Status:      stringPointer(m.Status),
		GoalIDs:     goalIDs,
		LeadAgentID: stringPointer(m.LeadAgentID),
		TargetDate:  stringPointer(m.TargetDate),
		Color:       stringPointer(m.Color),
	}
}

func (m *projectResourceModel) apply(ctx context.Context, p *client.Project, diags *diag.Diagnostics) {
	m.ID = types.StringValue(p.ID)
	m.CompanyID = types.StringValue(p.CompanyID)
	m.Name = types.StringValue(p.Name)
	m.Description = types.StringPointerValue(p.Description)
	m.Status = types.StringValue(p.Status)
	goalIDs := p.GoalIDs
	if goalIDs == nil {
		goalIDs = []string{}
	}
	set, d := types.SetValueFrom(ctx, types.StringType, goalIDs)
	diags.Append(d...)
	m.GoalIDs = set
	m.LeadAgentID = types.StringPointerValue(p.LeadAgentID)
	m.TargetDate = types.StringPointerValue(p.TargetDate)
	m.Color = types.StringPointerValue(p.Color)
	m.URLKey = types.StringValue(p.URLKey)
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := plan.write(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	project, err := r.client.CreateProject(ctx, plan.CompanyID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "create project", err)
		return
	}

	plan.apply(ctx, project, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project, err := r.client.GetProject(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read project", err)
		return
	}

	state.apply(ctx, project, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := plan.write(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	project, err := r.client.UpdateProject(ctx, plan.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "update project", err)
		return
	}

	plan.apply(ctx, project, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteProject(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete project", err)
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
