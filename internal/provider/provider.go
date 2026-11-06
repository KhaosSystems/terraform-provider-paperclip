// Package provider implements the Paperclip Terraform provider.
package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

const (
	envEndpoint = "PAPERCLIP_API_URL"
	envAPIKey   = "PAPERCLIP_API_KEY"
)

var _ provider.Provider = (*paperclipProvider)(nil)

type paperclipProvider struct {
	version string
}

type paperclipProviderModel struct {
	Endpoint   types.String `tfsdk:"endpoint"`
	APIKey     types.String `tfsdk:"api_key"`
	MaxRetries types.Int64  `tfsdk:"max_retries"`
}

// New returns a factory for the provider at the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &paperclipProvider{version: version}
	}
}

func (p *paperclipProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "paperclip"
	resp.Version = p.version
}

func (p *paperclipProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Paperclip provider manages companies, agents, goals and projects on a " +
			"[Paperclip](https://paperclip.ing) instance, so that an AI agent organisation can be " +
			"declared, reviewed and versioned as code.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Base URL of the Paperclip instance, for example `http://localhost:3100`. " +
					"Can also be set with the `" + envEndpoint + "` environment variable.",
			},
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Board API key used to authenticate. Not needed for instances that run " +
					"in the `local_trusted` deployment mode. Can also be set with the `" + envAPIKey +
					"` environment variable.",
			},
			"max_retries": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Number of times a request is retried after a transient failure " +
					"(rate limiting or a temporarily unavailable server). Defaults to `3`; `0` disables retries.",
			},
		},
	}
}

func (p *paperclipProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config paperclipProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Unknown Paperclip endpoint",
			"The provider cannot be configured because the endpoint is not known yet. "+
				"Set it to a static value or use the "+envEndpoint+" environment variable.")
	}
	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Unknown Paperclip API key",
			"The provider cannot be configured because the API key is not known yet. "+
				"Set it to a static value or use the "+envAPIKey+" environment variable.")
	}
	if config.MaxRetries.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("max_retries"), "Unknown max_retries",
			"The provider cannot be configured because max_retries is not known yet.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := os.Getenv(envEndpoint)
	if !config.Endpoint.IsNull() {
		endpoint = config.Endpoint.ValueString()
	}
	apiKey := os.Getenv(envAPIKey)
	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Missing Paperclip endpoint",
			"Set the endpoint attribute in the provider block or the "+envEndpoint+" environment variable "+
				"to the base URL of the Paperclip instance, for example http://localhost:3100.")
		return
	}

	// client.Config treats zero as "use the default" and a negative value as
	// "never retry", so an explicit 0 is mapped to -1.
	maxRetries := 0
	if !config.MaxRetries.IsNull() {
		v := config.MaxRetries.ValueInt64()
		switch {
		case v < 0 || v > 20:
			resp.Diagnostics.AddAttributeError(path.Root("max_retries"), "Invalid max_retries",
				fmt.Sprintf("max_retries must be between 0 and 20, got %d.", v))
			return
		case v == 0:
			maxRetries = -1
		default:
			maxRetries = int(v)
		}
	}

	c, err := client.New(client.Config{
		Endpoint:   endpoint,
		APIKey:     apiKey,
		UserAgent:  fmt.Sprintf("terraform-provider-paperclip/%s (terraform %s)", p.version, req.TerraformVersion),
		MaxRetries: maxRetries,
	})
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid Paperclip provider configuration", err.Error())
		return
	}

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *paperclipProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewCompanyResource,
		NewAgentResource,
		NewGoalResource,
		NewProjectResource,
	}
}

func (p *paperclipProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewCompanyDataSource,
		NewAgentDataSource,
		NewAgentsDataSource,
	}
}
