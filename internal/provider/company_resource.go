package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

var (
	_ resource.Resource                = (*companyResource)(nil)
	_ resource.ResourceWithConfigure   = (*companyResource)(nil)
	_ resource.ResourceWithImportState = (*companyResource)(nil)
)

// NewCompanyResource returns the paperclip_company resource.
func NewCompanyResource() resource.Resource {
	return &companyResource{}
}

type companyResource struct {
	client *client.Client
}

type companyResourceModel struct {
	ID                               types.String `tfsdk:"id"`
	Name                             types.String `tfsdk:"name"`
	Description                      types.String `tfsdk:"description"`
	BudgetMonthlyCents               types.Int64  `tfsdk:"budget_monthly_cents"`
	RequireBoardApprovalForNewAgents types.Bool   `tfsdk:"require_board_approval_for_new_agents"`
	Status                           types.String `tfsdk:"status"`
	IssuePrefix                      types.String `tfsdk:"issue_prefix"`
}

func (r *companyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_company"
}

func (r *companyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Paperclip company, the top-level container for agents, goals and projects.\n\n" +
			"~> Destroying this resource permanently deletes the company together with everything in it, " +
			"including agents, tasks and history that were not created by Terraform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the company.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the company.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "What the company does. Agents see this as their mission.",
			},
			"budget_monthly_cents": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(0),
				MarkdownDescription: "Monthly spending limit for the whole company, in cents. `0` (the default) means no limit.",
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
			},
			"require_board_approval_for_new_agents": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether new agents need board approval before they exist. " +
					"`paperclip_agent` creates agents directly, which the API only allows when this is `false`. " +
					"Defaults to the value chosen by the Paperclip instance.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Lifecycle status of the company: `active`, `paused` or `archived`.",
			},
			"issue_prefix": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Prefix used for the identifiers of the company's tasks.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *companyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *companyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan companyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	company, err := r.client.CreateCompany(ctx, client.CompanyCreate{
		Name:               plan.Name.ValueString(),
		Description:        stringPointer(plan.Description),
		BudgetMonthlyCents: int64Pointer(plan.BudgetMonthlyCents),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "create company", err)
		return
	}

	// Save the identifier straight away so that a failure in the follow-up
	// call below leaves a tracked (tainted) resource instead of an orphan.
	plan.ID = types.StringValue(company.ID)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), plan.ID)...)

	// The approval policy is not part of the create request.
	if want := boolPointer(plan.RequireBoardApprovalForNewAgents); want != nil && *want != company.RequireBoardApprovalForNewAgents {
		company, err = r.client.UpdateCompany(ctx, company.ID, client.CompanyUpdate{
			Description:                      stringPointer(plan.Description),
			RequireBoardApprovalForNewAgents: want,
		})
		if err != nil {
			addAPIError(&resp.Diagnostics, "set the agent approval policy of the new company", err)
			return
		}
	}

	plan.apply(company)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *companyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state companyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	company, err := r.client.GetCompany(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read company", err)
		return
	}

	state.apply(company)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *companyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan companyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	company, err := r.client.UpdateCompany(ctx, plan.ID.ValueString(), client.CompanyUpdate{
		Name:                             stringPointer(plan.Name),
		Description:                      stringPointer(plan.Description),
		BudgetMonthlyCents:               int64Pointer(plan.BudgetMonthlyCents),
		RequireBoardApprovalForNewAgents: boolPointer(plan.RequireBoardApprovalForNewAgents),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "update company", err)
		return
	}

	plan.apply(company)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *companyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state companyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteCompany(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete company", err)
	}
}

func (r *companyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (m *companyResourceModel) apply(c *client.Company) {
	m.ID = types.StringValue(c.ID)
	m.Name = types.StringValue(c.Name)
	m.Description = types.StringPointerValue(c.Description)
	m.BudgetMonthlyCents = types.Int64Value(c.BudgetMonthlyCents)
	m.RequireBoardApprovalForNewAgents = types.BoolValue(c.RequireBoardApprovalForNewAgents)
	m.Status = types.StringValue(c.Status)
	m.IssuePrefix = types.StringValue(c.IssuePrefix)
}
