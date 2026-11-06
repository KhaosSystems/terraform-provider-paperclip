package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

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
	_ resource.Resource                   = (*skillResource)(nil)
	_ resource.ResourceWithConfigure      = (*skillResource)(nil)
	_ resource.ResourceWithImportState    = (*skillResource)(nil)
	_ resource.ResourceWithValidateConfig = (*skillResource)(nil)
)

// NewSkillResource returns the paperclip_skill resource.
func NewSkillResource() resource.Resource {
	return &skillResource{}
}

type skillResource struct {
	client *client.Client
}

type skillResourceModel struct {
	ID               types.String `tfsdk:"id"`
	CompanyID        types.String `tfsdk:"company_id"`
	Name             types.String `tfsdk:"name"`
	Slug             types.String `tfsdk:"slug"`
	Description      types.String `tfsdk:"description"`
	Markdown         types.String `tfsdk:"markdown"`
	Key              types.String `tfsdk:"key"`
	CurrentVersionID types.String `tfsdk:"current_version_id"`
}

func (r *skillResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_skill"
}

func (r *skillResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a skill in a company's library. Assign it to agents through the `skills` attribute of " +
			"`paperclip_agent`.\n\nOnly the skill's `SKILL.md` is managed. Skills from other sources, such as GitHub, " +
			"cannot be edited this way.\n\n" +
			"~> Destroying this resource first detaches the skill from every agent that uses it, including agents " +
			"Terraform does not manage, and reports them in a warning.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the skill.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"company_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the company whose library holds the skill. Changing this replaces the skill.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the skill. Must match `name` in the frontmatter of `markdown`, when that is set.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.RegexMatches(regexp.MustCompile(`^[^\r\n]+$`), "must be a single line"),
				},
			},
			"slug": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "URL-safe identifier, derived from `name` when omitted. It forms part of `key`, so it does " +
					"not follow later renames. Changing this replaces the skill.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`), "must be lowercase letters, digits and hyphens"),
				},
			},
			"description": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "What the skill does. Must match `description` in the frontmatter of `markdown`, when that is " +
					"set. Omit it to take the value from the frontmatter.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"markdown": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Content of the skill's `SKILL.md`, typically loaded with `file()`.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`\S`), "must not be blank"),
				},
			},
			"key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Key that agents use to refer to the skill.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"current_version_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the skill's current version.",
			},
		},
	}
}

func (r *skillResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// ValidateConfig catches a mismatch between the attributes and the frontmatter.
// Paperclip lets the frontmatter win, which would otherwise surface as a
// confusing error after the apply.
func (r *skillResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config skillResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Markdown.IsNull() || config.Markdown.IsUnknown() {
		return
	}

	for key, attribute := range map[string]types.String{"name": config.Name, "description": config.Description} {
		if attribute.IsNull() || attribute.IsUnknown() {
			continue
		}
		if value, ok := frontmatterValue(config.Markdown.ValueString(), key); ok && value != attribute.ValueString() {
			resp.Diagnostics.AddAttributeError(path.Root(key), "Attribute differs from the skill's frontmatter",
				fmt.Sprintf("%s is %q, but the frontmatter of markdown says %q. Paperclip uses the frontmatter, so make them match.",
					key, attribute.ValueString(), value))
		}
	}
}

func (r *skillResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan skillResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	skill, err := r.client.CreateSkill(ctx, plan.CompanyID.ValueString(), client.SkillCreate{
		Name:        plan.Name.ValueString(),
		Slug:        stringPointer(plan.Slug),
		Description: stringPointer(plan.Description),
		Markdown:    plan.Markdown.ValueString(),
	})
	if err != nil {
		addAPIError(&resp.Diagnostics, "create skill", err)
		return
	}

	plan.apply(skill)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *skillResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state skillResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	skill, err := r.client.GetSkill(ctx, state.CompanyID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read skill", err)
		return
	}

	state.apply(skill)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *skillResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state skillResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	companyID, id := plan.CompanyID.ValueString(), plan.ID.ValueString()

	if !plan.Name.Equal(state.Name) {
		if err := r.client.RenameSkill(ctx, companyID, id, plan.Name.ValueString(), state.Slug.ValueString()); err != nil {
			addAPIError(&resp.Diagnostics, "rename skill", err)
			return
		}
	}

	// Compare with the server, because a rename can rewrite the frontmatter.
	skill, err := r.client.GetSkill(ctx, companyID, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "read skill", err)
		return
	}
	if skill.Markdown != plan.Markdown.ValueString() {
		if err := r.client.UpdateSkillMarkdown(ctx, companyID, id, plan.Markdown.ValueString()); err != nil {
			addAPIError(&resp.Diagnostics, "update skill content", err)
			return
		}
	}

	if !plan.Description.IsUnknown() && !plan.Description.Equal(state.Description) {
		if err := r.client.UpdateSkillDescription(ctx, companyID, id, plan.Description.ValueString()); err != nil {
			addAPIError(&resp.Diagnostics, "update skill description", err)
			return
		}
	}

	skill, err = r.client.GetSkill(ctx, companyID, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "read skill", err)
		return
	}
	plan.apply(skill)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *skillResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state skillResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	companyID, id := state.CompanyID.ValueString(), state.ID.ValueString()

	// Paperclip refuses to delete a skill in use. Terraform deletes the skill
	// before it updates the agents that used to refer to it, so without this
	// removing a skill and its assignments in one apply could never succeed.
	skill, err := r.client.GetSkill(ctx, companyID, id)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "read skill", err)
		return
	}
	names := make([]string, 0, len(skill.UsedByAgents))
	for _, agent := range skill.UsedByAgents {
		if err := r.client.DetachSkill(ctx, agent.ID, skill.Key); err != nil {
			addAPIError(&resp.Diagnostics, fmt.Sprintf("detach skill from agent %q", agent.Name), err)
			return
		}
		names = append(names, agent.Name)
	}
	if len(names) > 0 {
		resp.Diagnostics.AddWarning("Skill detached from agents",
			fmt.Sprintf("Skill %q was removed from these agents before it was deleted: %s.", skill.Name, strings.Join(names, ", ")))
	}

	if err := r.client.DeleteSkill(ctx, companyID, id); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "delete skill", err)
	}
}

// ImportState accepts "company_id/skill_id", because the API addresses a skill
// through its company.
func (r *skillResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	companyID, id, ok := strings.Cut(req.ID, "/")
	if !ok || companyID == "" || id == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected company_id/skill_id, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("company_id"), companyID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

func (m *skillResourceModel) apply(s *client.Skill) {
	m.ID = types.StringValue(s.ID)
	m.CompanyID = types.StringValue(s.CompanyID)
	m.Name = types.StringValue(s.Name)
	m.Slug = types.StringValue(s.Slug)
	m.Description = types.StringPointerValue(s.Description)
	m.Markdown = types.StringValue(s.Markdown)
	m.Key = types.StringValue(s.Key)
	m.CurrentVersionID = types.StringPointerValue(s.CurrentVersionID)
}
