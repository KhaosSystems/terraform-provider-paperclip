package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

var (
	_ datasource.DataSource              = (*companyDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*companyDataSource)(nil)
)

// NewCompanyDataSource returns the paperclip_company data source.
func NewCompanyDataSource() datasource.DataSource {
	return &companyDataSource{}
}

type companyDataSource struct {
	client *client.Client
}

type companyDataSourceModel struct {
	ID                               types.String `tfsdk:"id"`
	Name                             types.String `tfsdk:"name"`
	Description                      types.String `tfsdk:"description"`
	BudgetMonthlyCents               types.Int64  `tfsdk:"budget_monthly_cents"`
	SpentMonthlyCents                types.Int64  `tfsdk:"spent_monthly_cents"`
	RequireBoardApprovalForNewAgents types.Bool   `tfsdk:"require_board_approval_for_new_agents"`
	Status                           types.String `tfsdk:"status"`
	IssuePrefix                      types.String `tfsdk:"issue_prefix"`
}

func (d *companyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_company"
}

func (d *companyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing Paperclip company by `id` or by `name`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Identifier of the company. Exactly one of `id` and `name` must be set.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
				},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Exact name of the company. The lookup fails when several companies share the name.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "What the company does.",
			},
			"budget_monthly_cents": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Monthly spending limit for the whole company, in cents. `0` means no limit.",
			},
			"spent_monthly_cents": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Amount spent in the current month, in cents.",
			},
			"require_board_approval_for_new_agents": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether new agents need board approval before they exist.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Lifecycle status of the company: `active`, `paused` or `archived`.",
			},
			"issue_prefix": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Prefix used for the identifiers of the company's tasks.",
			},
		},
	}
}

func (d *companyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *companyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config companyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var company *client.Company
	if !config.ID.IsNull() {
		var err error
		company, err = d.client.GetCompany(ctx, config.ID.ValueString())
		if err != nil {
			addAPIError(&resp.Diagnostics, "read company", err)
			return
		}
	} else {
		companies, err := d.client.ListCompanies(ctx)
		if err != nil {
			addAPIError(&resp.Diagnostics, "list companies", err)
			return
		}
		name := config.Name.ValueString()
		var matches []client.Company
		for _, c := range companies {
			if c.Name == name {
				matches = append(matches, c)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Company not found",
				fmt.Sprintf("No company is named %q.", name))
			return
		case 1:
			company = &matches[0]
		default:
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Ambiguous company name",
				fmt.Sprintf("%d companies are named %q. Look the company up by id instead.", len(matches), name))
			return
		}
	}

	state := companyDataSourceModel{
		ID:                               types.StringValue(company.ID),
		Name:                             types.StringValue(company.Name),
		Description:                      types.StringPointerValue(company.Description),
		BudgetMonthlyCents:               types.Int64Value(company.BudgetMonthlyCents),
		SpentMonthlyCents:                types.Int64Value(company.SpentMonthlyCents),
		RequireBoardApprovalForNewAgents: types.BoolValue(company.RequireBoardApprovalForNewAgents),
		Status:                           types.StringValue(company.Status),
		IssuePrefix:                      types.StringValue(company.IssuePrefix),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
