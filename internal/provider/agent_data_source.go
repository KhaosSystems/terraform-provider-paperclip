package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

var (
	_ datasource.DataSource              = (*agentDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*agentDataSource)(nil)
	_ datasource.DataSource              = (*agentsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*agentsDataSource)(nil)
)

// agentSummaryModel is the read-only view of an agent shared by the agent
// data sources. Adapter and runtime settings are left out on purpose: the API
// redacts parts of them, so they are of little use as data.
type agentSummaryModel struct {
	ID                 types.String `tfsdk:"id"`
	CompanyID          types.String `tfsdk:"company_id"`
	Name               types.String `tfsdk:"name"`
	URLKey             types.String `tfsdk:"url_key"`
	Role               types.String `tfsdk:"role"`
	Title              types.String `tfsdk:"title"`
	Status             types.String `tfsdk:"status"`
	ReportsTo          types.String `tfsdk:"reports_to"`
	Capabilities       types.String `tfsdk:"capabilities"`
	AdapterType        types.String `tfsdk:"adapter_type"`
	BudgetMonthlyCents types.Int64  `tfsdk:"budget_monthly_cents"`
	SpentMonthlyCents  types.Int64  `tfsdk:"spent_monthly_cents"`
}

func agentSummaryAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":                   types.StringType,
		"company_id":           types.StringType,
		"name":                 types.StringType,
		"url_key":              types.StringType,
		"role":                 types.StringType,
		"title":                types.StringType,
		"status":               types.StringType,
		"reports_to":           types.StringType,
		"capabilities":         types.StringType,
		"adapter_type":         types.StringType,
		"budget_monthly_cents": types.Int64Type,
		"spent_monthly_cents":  types.Int64Type,
	}
}

func newAgentSummaryModel(a *client.Agent) agentSummaryModel {
	return agentSummaryModel{
		ID:                 types.StringValue(a.ID),
		CompanyID:          types.StringValue(a.CompanyID),
		Name:               types.StringValue(a.Name),
		URLKey:             types.StringValue(a.URLKey),
		Role:               types.StringValue(a.Role),
		Title:              types.StringPointerValue(a.Title),
		Status:             types.StringValue(a.Status),
		ReportsTo:          types.StringPointerValue(a.ReportsTo),
		Capabilities:       types.StringPointerValue(a.Capabilities),
		AdapterType:        types.StringValue(a.AdapterType),
		BudgetMonthlyCents: types.Int64Value(a.BudgetMonthlyCents),
		SpentMonthlyCents:  types.Int64Value(a.SpentMonthlyCents),
	}
}

// agentComputedAttributes returns the read-only attributes shared by both
// agent data sources.
func agentComputedAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"url_key": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "URL-safe key of the agent, derived from its name.",
		},
		"role": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Role of the agent in the organisation.",
		},
		"title": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Job title shown in the organisation chart.",
		},
		"status": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Current status of the agent, such as `idle`, `running`, `paused` or `error`.",
		},
		"reports_to": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the agent this agent reports to.",
		},
		"capabilities": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Free-form description of what the agent is good at.",
		},
		"adapter_type": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Adapter that runs the agent.",
		},
		"budget_monthly_cents": schema.Int64Attribute{
			Computed:            true,
			MarkdownDescription: "Monthly spending limit for the agent, in cents. `0` means no limit.",
		},
		"spent_monthly_cents": schema.Int64Attribute{
			Computed:            true,
			MarkdownDescription: "Amount the agent spent in the current month, in cents.",
		},
	}
}

// NewAgentDataSource returns the paperclip_agent data source.
func NewAgentDataSource() datasource.DataSource {
	return &agentDataSource{}
}

type agentDataSource struct {
	client *client.Client
}

func (d *agentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (d *agentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := agentComputedAttributes()
	attributes["id"] = schema.StringAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: "Identifier of the agent. Exactly one of `id` and `name` must be set.",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
			stringvalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
		},
	}
	attributes["name"] = schema.StringAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: "Exact name of the agent. Requires `company_id`.",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
			stringvalidator.AlsoRequires(path.MatchRoot("company_id")),
		},
	}
	attributes["company_id"] = schema.StringAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: "Identifier of the company the agent works for. Required when looking the agent up by `name`.",
		Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing Paperclip agent by `id`, or by `name` within a company.",
		Attributes:          attributes,
	}
}

func (d *agentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *agentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config agentSummaryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var agent *client.Agent
	if !config.ID.IsNull() {
		var err error
		agent, err = d.client.GetAgent(ctx, config.ID.ValueString())
		if err != nil {
			addAPIError(&resp.Diagnostics, "read agent", err)
			return
		}
		if !config.CompanyID.IsNull() && config.CompanyID.ValueString() != agent.CompanyID {
			resp.Diagnostics.AddAttributeError(path.Root("company_id"), "Agent belongs to another company",
				fmt.Sprintf("Agent %s belongs to company %s, not %s.", agent.ID, agent.CompanyID, config.CompanyID.ValueString()))
			return
		}
	} else {
		agents, err := d.client.ListAgents(ctx, config.CompanyID.ValueString())
		if err != nil {
			addAPIError(&resp.Diagnostics, "list agents", err)
			return
		}
		name := config.Name.ValueString()
		var matches []client.Agent
		for _, a := range agents {
			if a.Name == name {
				matches = append(matches, a)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Agent not found",
				fmt.Sprintf("No agent in company %s is named %q.", config.CompanyID.ValueString(), name))
			return
		case 1:
			agent = &matches[0]
		default:
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Ambiguous agent name",
				fmt.Sprintf("%d agents are named %q. Look the agent up by id instead.", len(matches), name))
			return
		}
	}

	state := newAgentSummaryModel(agent)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// NewAgentsDataSource returns the paperclip_agents data source.
func NewAgentsDataSource() datasource.DataSource {
	return &agentsDataSource{}
}

type agentsDataSource struct {
	client *client.Client
}

type agentsDataSourceModel struct {
	CompanyID types.String `tfsdk:"company_id"`
	Agents    types.List   `tfsdk:"agents"`
}

func (d *agentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agents"
}

func (d *agentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	nested := agentComputedAttributes()
	nested["id"] = schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: "Identifier of the agent.",
	}
	nested["name"] = schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: "Name of the agent.",
	}
	nested["company_id"] = schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: "Identifier of the company the agent works for.",
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the agents of a Paperclip company.",
		Attributes: map[string]schema.Attribute{
			"company_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the company whose agents are listed.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"agents": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Agents of the company, ordered by name.",
				NestedObject:        schema.NestedAttributeObject{Attributes: nested},
			},
		},
	}
}

func (d *agentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *agentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config agentsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	agents, err := d.client.ListAgents(ctx, config.CompanyID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "list agents", err)
		return
	}

	sortAgentsByName(agents)
	models := make([]agentSummaryModel, 0, len(agents))
	for i := range agents {
		models = append(models, newAgentSummaryModel(&agents[i]))
	}

	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: agentSummaryAttrTypes()}, models)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Agents = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
