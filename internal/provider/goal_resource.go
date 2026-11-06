package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

var (
	_ resource.Resource                = (*goalResource)(nil)
	_ resource.ResourceWithConfigure   = (*goalResource)(nil)
	_ resource.ResourceWithImportState = (*goalResource)(nil)
)

// NewGoalResource returns the paperclip_goal resource.
func NewGoalResource() resource.Resource {
	return &goalResource{}
}

type goalResource struct {
	client *client.Client
}

type goalResourceModel struct {
	ID           types.String `tfsdk:"id"`
	CompanyID    types.String `tfsdk:"company_id"`
	Title        types.String `tfsdk:"title"`
	Description  types.String `tfsdk:"description"`
	Level        types.String `tfsdk:"level"`
	Status       types.String `tfsdk:"status"`
	ParentID     types.String `tfsdk:"parent_id"`
	OwnerAgentID types.String `tfsdk:"owner_agent_id"`
}

func (r *goalResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_goal"
}

func (r *goalResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Paperclip goal. Goals form a tree from the company mission down to " +
			"individual tasks and give agents the reason behind their work.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the goal.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"company_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the company the goal belongs to. Changing this replaces the goal.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"title": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Short statement of the goal.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Longer explanation of the goal.",
			},
			"level": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Scope of the goal: `company`, `team`, `agent` or `task`. Defaults to `task`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"status": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Progress of the goal: `planned`, `active`, `achieved` or `cancelled`. Defaults to `planned`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"parent_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Identifier of the parent goal. Omit for a top-level goal.",
			},
			"owner_agent_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Identifier of the agent that owns the goal.",
			},
		},
	}
}

func (r *goalResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (m *goalResourceModel) write() client.GoalWrite {
	return client.GoalWrite{
		Title:        m.Title.ValueString(),
		Description:  stringPointer(m.Description),
		Level:        stringPointer(m.Level),
		Status:       stringPointer(m.Status),
		ParentID:     stringPointer(m.ParentID),
		OwnerAgentID: stringPointer(m.OwnerAgentID),
	}
}

func (m *goalResourceModel) apply(g *client.Goal) {
	m.ID = types.StringValue(g.ID)
	m.CompanyID = types.StringValue(g.CompanyID)
	m.Title = types.StringValue(g.Title)
	m.Description = types.StringPointerValue(g.Description)
	m.Level = types.StringValue(g.Level)
	m.Status = types.StringValue(g.Status)
	m.ParentID = types.StringPointerValue(g.ParentID)
	m.OwnerAgentID = types.StringPointerValue(g.OwnerAgentID)
}

func (r *goalResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan goalResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	goal, err := r.client.CreateGoal(ctx, plan.CompanyID.ValueString(), plan.write())
	if err != nil {
		addAPIError(&resp.Diagnostics, "create goal", err)
		return
	}

	plan.apply(goal)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *goalResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state goalResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	goal, err := r.client.GetGoal(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read goal", err)
		return
	}

	state.apply(goal)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *goalResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan goalResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	goal, err := r.client.UpdateGoal(ctx, plan.ID.ValueString(), plan.write())
	if err != nil {
		addAPIError(&resp.Diagnostics, "update goal", err)
		return
	}

	plan.apply(goal)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *goalResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state goalResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteGoal(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete goal", err)
	}
}

func (r *goalResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
