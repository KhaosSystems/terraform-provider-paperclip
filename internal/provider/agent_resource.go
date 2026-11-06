package provider

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
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
	Instructions       types.String         `tfsdk:"instructions"`
	Skills             types.Set            `tfsdk:"skills"`
	CanCreateAgents    types.Bool           `tfsdk:"can_create_agents"`
	CanCreateSkills    types.Bool           `tfsdk:"can_create_skills"`
	Status             types.String         `tfsdk:"status"`
	URLKey             types.String         `tfsdk:"url_key"`
}

// defaultInstructionsEntry is the entry file of a new instruction bundle.
const defaultInstructionsEntry = "AGENTS.md"

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
			"instructions": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Content of the agent's instruction entry file, usually `AGENTS.md`, typically loaded with `file()`. " +
					"Only adapters with managed instruction bundles support this, such as `codex_local` and `opencode_local`; " +
					"`process` and `http` do not. When omitted, Paperclip's default instructions are left alone, and removing " +
					"the attribute stops tracking the file without changing it. Agents can edit their own instructions, " +
					"which shows up as drift. Other files in the bundle are not managed.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"skills": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				MarkdownDescription: "Complete set of skills assigned to the agent, as skill keys such as " +
					"`paperclip_skill.example.key`. Skills assigned any other way are removed. An empty set removes all " +
					"skills. When omitted, assignments are left alone, and removing the attribute stops tracking them " +
					"without changing them.",
			},
			"can_create_agents": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether the agent may hire other agents. Paperclip normally allows this by default, " +
					"so set it to `false` to keep the organisation as declared.",
			},
			"can_create_skills": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the agent may add skills to the company library. Paperclip allows this by default.",
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

	adapterConfig := decodeJSONObject(config.AdapterConfig, path.Root("adapter_config"), &resp.Diagnostics)
	for _, key := range serverManagedAdapterKeys {
		if _, declared := adapterConfig[key]; !declared {
			continue
		}
		if key == "paperclipSkillSync" && !config.Skills.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("adapter_config"), "Conflicting skill configuration",
				"adapter_config must not set paperclipSkillSync together with the skills attribute.")
		}
		if key != "paperclipSkillSync" && !config.Instructions.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("adapter_config"), "Conflicting instructions configuration",
				"adapter_config must not set "+key+" together with the instructions attribute.")
		}
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
	// Everything is sent in the create call, so that the agent never exists
	// without its permissions, for example able to hire.
	createAgents, createSkills := boolPointer(plan.CanCreateAgents), boolPointer(plan.CanCreateSkills)
	if createAgents != nil || createSkills != nil {
		in.Permissions = &client.AgentPermissionsCreate{CanCreateAgents: createAgents, CanCreateSkills: createSkills}
	}
	if isSet(plan.Skills) {
		in.DesiredSkills = []string{}
		resp.Diagnostics.Append(plan.Skills.ElementsAs(ctx, &in.DesiredSkills, false)...)
	}
	if want := stringPointer(plan.Instructions); want != nil {
		in.InstructionsBundle = &client.AgentInstructionsBundleCreate{
			EntryFile: defaultInstructionsEntry,
			Files:     map[string]string{defaultInstructionsEntry: *want},
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateAgent(ctx, plan.CompanyID.ValueString(), in)
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

	// Save the identifier straight away, so that a failure below leaves a
	// tracked (tainted) resource instead of an orphan.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.StringValue(created.ID))...)

	r.finish(ctx, created.ID, &plan, &resp.State, &resp.Diagnostics)
}

// finish reads the agent back, checks the instructions arrived and saves the
// result. Only the single-agent read returns everything the state needs.
func (r *agentResource) finish(ctx context.Context, id string, plan *agentResourceModel, state *tfsdk.State, diags *diag.Diagnostics) {
	agent, err := r.client.GetAgent(ctx, id)
	if err != nil {
		addAPIError(diags, "read agent", err)
		return
	}

	if want := stringPointer(plan.Instructions); want != nil {
		got, found, err := r.client.GetAgentInstructions(ctx, id)
		if err != nil {
			addAPIError(diags, "read agent instructions", err)
			return
		}
		if !found || got != *want {
			diags.AddAttributeError(path.Root("instructions"), "Instructions were not stored",
				fmt.Sprintf("Paperclip did not keep the declared instructions. Adapter %q may not support managed "+
					"instruction bundles; process and http do not. Remove the instructions attribute or use an adapter "+
					"that supports them.", agent.AdapterType))
			return
		}
	}

	plan.apply(ctx, agent, diags)
	diags.Append(state.Set(ctx, plan)...)
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

	// Instructions are only read when managed: the default files are not ours.
	if !state.Instructions.IsNull() {
		content, found, err := r.client.GetAgentInstructions(ctx, agent.ID)
		if err != nil {
			addAPIError(&resp.Diagnostics, "read agent instructions", err)
			return
		}
		state.Instructions = types.StringNull()
		if found {
			state.Instructions = types.StringValue(content)
		}
	}

	state.apply(ctx, agent, &resp.Diagnostics)
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

	current, err := r.client.GetAgent(ctx, plan.ID.ValueString())
	if err != nil {
		addAPIError(&resp.Diagnostics, "read agent", err)
		return
	}

	// The JSON settings are only sent when they changed. An unchanged value
	// may hold redacted placeholders read back from the API, and sending
	// those would overwrite the real secrets. Changing the adapter type resets
	// the settings on the server, so they are sent then too.
	if jsonObjectChanged(plan.AdapterConfig, state.AdapterConfig, path.Root("adapter_config"), &resp.Diagnostics) ||
		(!plan.AdapterType.Equal(state.AdapterType) && !plan.AdapterConfig.IsUnknown()) {
		declared := decodeJSONObject(plan.AdapterConfig, path.Root("adapter_config"), &resp.Diagnostics)
		in.AdapterConfig = withServerManagedKeys(declared, current.AdapterConfig)
		in.ReplaceAdapterConfig = true
	}
	if jsonObjectChanged(plan.RuntimeConfig, state.RuntimeConfig, path.Root("runtime_config"), &resp.Diagnostics) {
		in.RuntimeConfig = orEmpty(decodeJSONObject(plan.RuntimeConfig, path.Root("runtime_config"), &resp.Diagnostics))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.UpdateAgent(ctx, plan.ID.ValueString(), in); err != nil {
		addAPIError(&resp.Diagnostics, "update agent", err)
		return
	}

	if update, changed := permissionsUpdate(&plan, current); changed {
		if err := r.client.UpdateAgentPermissions(ctx, plan.ID.ValueString(), update); err != nil {
			addAPIError(&resp.Diagnostics, "update agent permissions", err)
			return
		}
	}

	if isSet(plan.Skills) {
		var declared []string
		resp.Diagnostics.Append(plan.Skills.ElementsAs(ctx, &declared, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if !skillSetsEqual(declared, current.DesiredSkills()) {
			if err := r.client.SyncAgentSkills(ctx, plan.ID.ValueString(), declared); err != nil {
				addAPIError(&resp.Diagnostics, "update agent skills", err)
				return
			}
		}
	}

	if want := stringPointer(plan.Instructions); want != nil {
		if err := r.client.SetAgentInstructions(ctx, plan.ID.ValueString(), *want); err != nil {
			addAPIError(&resp.Diagnostics, "update agent instructions", err)
			return
		}
	}

	r.finish(ctx, plan.ID.ValueString(), &plan, &resp.State, &resp.Diagnostics)
}

// permissionsUpdate builds the permissions request and reports whether anything
// declared differs from the current values.
//
// The API requires canAssignTasks on every call. It is not exposed as an
// attribute because the read value cannot confirm it: every active agent reads
// back as able to assign tasks. The existing grant is sent back unchanged.
func permissionsUpdate(plan *agentResourceModel, current *client.Agent) (client.AgentPermissionsUpdate, bool) {
	update := client.AgentPermissionsUpdate{CanAssignTasks: current.HasTaskAssignGrant()}
	if current.Permissions != nil {
		update.CanCreateAgents = current.Permissions.CanCreateAgents
	}

	changed := false
	if want := boolPointer(plan.CanCreateAgents); want != nil && *want != update.CanCreateAgents {
		update.CanCreateAgents = *want
		changed = true
	}
	if want := boolPointer(plan.CanCreateSkills); want != nil {
		update.CanCreateSkills = want
		if current.Permissions == nil || current.Permissions.CanCreateSkills == nil || *current.Permissions.CanCreateSkills != *want {
			changed = true
		}
	}
	return update, changed
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
func (m *agentResourceModel) apply(ctx context.Context, a *client.Agent, diags *diag.Diagnostics) {
	m.ID = types.StringValue(a.ID)
	m.CompanyID = types.StringValue(a.CompanyID)
	m.Name = types.StringValue(a.Name)
	m.Role = types.StringValue(a.Role)
	m.Title = types.StringPointerValue(a.Title)
	m.ReportsTo = types.StringPointerValue(a.ReportsTo)
	m.Capabilities = types.StringPointerValue(a.Capabilities)
	m.AdapterType = types.StringValue(a.AdapterType)
	m.AdapterConfig = adapterConfigState(a.AdapterConfig, m.AdapterConfig, path.Root("adapter_config"), diags)
	m.RuntimeConfig = jsonObjectState(a.RuntimeConfig, m.RuntimeConfig, path.Root("runtime_config"), diags)
	m.BudgetMonthlyCents = types.Int64Value(a.BudgetMonthlyCents)
	if a.Metadata == nil {
		m.Metadata = jsontypes.NewNormalizedNull()
	} else {
		m.Metadata = encodeJSONObject(a.Metadata, path.Root("metadata"), diags)
	}
	m.Status = types.StringValue(a.Status)
	m.URLKey = types.StringValue(a.URLKey)

	m.CanCreateAgents, m.CanCreateSkills = types.BoolNull(), types.BoolNull()
	if a.Permissions != nil {
		m.CanCreateAgents = types.BoolValue(a.Permissions.CanCreateAgents)
		m.CanCreateSkills = types.BoolValue(true)
		if a.Permissions.CanCreateSkills != nil {
			m.CanCreateSkills = types.BoolValue(*a.Permissions.CanCreateSkills)
		}
	}

	// Assignments are only tracked when declared.
	if isSet(m.Skills) {
		var declared []string
		diags.Append(m.Skills.ElementsAs(ctx, &declared, false)...)
		skills, d := types.SetValueFrom(ctx, types.StringType, matchSkillReferences(declared, a.DesiredSkills()))
		diags.Append(d...)
		m.Skills = skills
	}
}

// isSet reports whether a set attribute has a known, non-null value.
func isSet(v types.Set) bool {
	return !v.IsNull() && !v.IsUnknown()
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
