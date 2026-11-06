package provider

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

var (
	_ resource.Resource                   = (*agentResource)(nil)
	_ resource.ResourceWithConfigure      = (*agentResource)(nil)
	_ resource.ResourceWithImportState    = (*agentResource)(nil)
	_ resource.ResourceWithValidateConfig = (*agentResource)(nil)
)

// NewAgentResource returns the paperclip_agent resource.
func NewAgentResource() resource.Resource {
	return &agentResource{}
}

type agentResource struct {
	client *client.Client
}

type agentResourceModel struct {
	ID                 types.String         `tfsdk:"id"`
	CompanyID          types.String         `tfsdk:"company_id"`
	Name               types.String         `tfsdk:"name"`
	Role               types.String         `tfsdk:"role"`
	Title              types.String         `tfsdk:"title"`
	ReportsTo          types.String         `tfsdk:"reports_to"`
	Capabilities       types.String         `tfsdk:"capabilities"`
	AdapterType        types.String         `tfsdk:"adapter_type"`
	AdapterConfig      jsontypes.Normalized `tfsdk:"adapter_config"`
	RuntimeConfig      jsontypes.Normalized `tfsdk:"runtime_config"`
	BudgetMonthlyCents types.Int64          `tfsdk:"budget_monthly_cents"`
	Metadata           jsontypes.Normalized `tfsdk:"metadata"`
	Status             types.String         `tfsdk:"status"`
	URLKey             types.String         `tfsdk:"url_key"`
}

const jsonObjectTrackingNote = "Only the keys you declare are tracked: defaults that Paperclip adds on its own " +
	"are ignored, and values the API redacts are assumed to be unchanged. When the attribute is omitted, the " +
	"value stored by Paperclip is kept as is."

func (r *agentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (r *agentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Paperclip agent, an AI employee of a company.\n\n" +
			"Agents are created directly, without a hire approval. The API only allows that when the company has " +
			"`require_board_approval_for_new_agents` set to `false`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the agent.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"company_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the company the agent works for. Changing this replaces the agent.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the agent.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"role": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Role of the agent in the organisation, such as `ceo`, `engineer`, `designer`, " +
					"`pm`, `qa`, `researcher` or `general`. Defaults to `general`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"title": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Job title shown in the organisation chart.",
			},
			"reports_to": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Identifier of the agent this agent reports to. Omit for the top of the organisation.",
			},
			"capabilities": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Free-form description of what the agent is good at. Other agents use it to decide who gets a task.",
			},
			"adapter_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Adapter that runs the agent, such as `process`, `http`, `codex_local` or `opencode_local`. " +
					"See the Paperclip adapter documentation for the full list.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"adapter_config": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Optional:   true,
				Computed:   true,
				MarkdownDescription: "Adapter-specific settings as a JSON object, usually written with `jsonencode()`. " +
					jsonObjectTrackingNote + "\n\n" +
					"~> Plain values under `env` are stored in the Terraform state. Prefer secret references for credentials.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"runtime_config": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{},
				Optional:   true,
				Computed:   true,
				MarkdownDescription: "Runtime settings as a JSON object, for example " +
					"`jsonencode({ heartbeat = { enabled = true, intervalSec = 300 } })`. " + jsonObjectTrackingNote,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"budget_monthly_cents": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(0),
				MarkdownDescription: "Monthly spending limit for the agent, in cents. `0` (the default) means no limit.",
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
			},
			"metadata": schema.StringAttribute{
				CustomType:          jsontypes.NormalizedType{},
				Optional:            true,
				MarkdownDescription: "Arbitrary metadata as a JSON object.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current status of the agent, such as `idle`, `running`, `paused` or `error`.",
			},
			"url_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL-safe key of the agent, derived from its name.",
			},
		},
	}
}

func (r *agentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (r *agentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config agentResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for attr, value := range map[string]jsontypes.Normalized{
		"adapter_config": config.AdapterConfig,
		"runtime_config": config.RuntimeConfig,
		"metadata":       config.Metadata,
	} {
		p := path.Root(attr)
		obj := decodeJSONObject(value, p, &resp.Diagnostics)
		if containsRedacted(obj) {
			resp.Diagnostics.AddAttributeError(p, "Redacted placeholder in configuration",
				"The value contains \""+client.RedactedValue+"\", the placeholder Paperclip returns instead of a secret. "+
					"Applying it would overwrite the real value with the placeholder. Replace it with the real value "+
					"or a secret reference, or remove the key.")
		}
	}
}

func (r *agentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan agentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.AgentCreate{
		Name:               plan.Name.ValueString(),
		Role:               stringPointer(plan.Role),
		Title:              stringPointer(plan.Title),
		ReportsTo:          stringPointer(plan.ReportsTo),
		Capabilities:       stringPointer(plan.Capabilities),
		AdapterType:        plan.AdapterType.ValueString(),
		AdapterConfig:      decodeJSONObject(plan.AdapterConfig, path.Root("adapter_config"), &resp.Diagnostics),
		RuntimeConfig:      decodeJSONObject(plan.RuntimeConfig, path.Root("runtime_config"), &resp.Diagnostics),
		BudgetMonthlyCents: int64Pointer(plan.BudgetMonthlyCents),
		Metadata:           decodeJSONObject(plan.Metadata, path.Root("metadata"), &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	agent, err := r.client.CreateAgent(ctx, plan.CompanyID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "create agent", err)
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
			resp.Diagnostics.AddAttributeWarning(path.Root("company_id"), "Company may require board approval for new agents",
				"Paperclip rejects direct agent creation while the company requires board approval for new agents. "+
					"Set require_board_approval_for_new_agents = false on the paperclip_company resource.")
		}
		return
	}

	plan.apply(agent, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *agentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	agent, err := r.client.GetAgent(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read agent", err)
		return
	}

	state.apply(agent, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *agentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state agentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.AgentUpdate{
		Name:               stringPointer(plan.Name),
		Role:               stringPointer(plan.Role),
		Title:              stringPointer(plan.Title),
		ReportsTo:          stringPointer(plan.ReportsTo),
		Capabilities:       stringPointer(plan.Capabilities),
		AdapterType:        stringPointer(plan.AdapterType),
		BudgetMonthlyCents: int64Pointer(plan.BudgetMonthlyCents),
		Metadata:           decodeJSONObject(plan.Metadata, path.Root("metadata"), &resp.Diagnostics),
	}

	// The JSON settings are only sent when they changed. An unchanged value
	// may hold redacted placeholders read back from the API, and sending
	// those would overwrite the real secrets.
	if jsonObjectChanged(plan.AdapterConfig, state.AdapterConfig, path.Root("adapter_config"), &resp.Diagnostics) {
		in.AdapterConfig = orEmpty(decodeJSONObject(plan.AdapterConfig, path.Root("adapter_config"), &resp.Diagnostics))
		in.ReplaceAdapterConfig = true
	}
	if jsonObjectChanged(plan.RuntimeConfig, state.RuntimeConfig, path.Root("runtime_config"), &resp.Diagnostics) {
		in.RuntimeConfig = orEmpty(decodeJSONObject(plan.RuntimeConfig, path.Root("runtime_config"), &resp.Diagnostics))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	agent, err := r.client.UpdateAgent(ctx, plan.ID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "update agent", err)
		return
	}

	plan.apply(agent, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *agentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteAgent(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete agent", err)
	}
}

func (r *agentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// apply copies the API representation into the model. The JSON attributes use
// the model's current values (the plan or the prior state) to decide which
// keys are tracked.
func (m *agentResourceModel) apply(a *client.Agent, diags *diag.Diagnostics) {
	m.ID = types.StringValue(a.ID)
	m.CompanyID = types.StringValue(a.CompanyID)
	m.Name = types.StringValue(a.Name)
	m.Role = types.StringValue(a.Role)
	m.Title = types.StringPointerValue(a.Title)
	m.ReportsTo = types.StringPointerValue(a.ReportsTo)
	m.Capabilities = types.StringPointerValue(a.Capabilities)
	m.AdapterType = types.StringValue(a.AdapterType)
	m.AdapterConfig = jsonObjectState(a.AdapterConfig, m.AdapterConfig, path.Root("adapter_config"), diags)
	m.RuntimeConfig = jsonObjectState(a.RuntimeConfig, m.RuntimeConfig, path.Root("runtime_config"), diags)
	m.BudgetMonthlyCents = types.Int64Value(a.BudgetMonthlyCents)
	if a.Metadata == nil {
		m.Metadata = jsontypes.NewNormalizedNull()
	} else {
		m.Metadata = encodeJSONObject(a.Metadata, path.Root("metadata"), diags)
	}
	m.Status = types.StringValue(a.Status)
	m.URLKey = types.StringValue(a.URLKey)
}

// jsonObjectChanged reports whether two JSON object attributes differ in
// meaning, ignoring formatting and key order.
func jsonObjectChanged(plan, state jsontypes.Normalized, attr path.Path, diags *diag.Diagnostics) bool {
	if plan.IsUnknown() {
		return false
	}
	if plan.IsNull() || state.IsNull() || state.IsUnknown() {
		return plan.IsNull() != state.IsNull()
	}
	return !reflect.DeepEqual(decodeJSONObject(plan, attr, diags), decodeJSONObject(state, attr, diags))
}
