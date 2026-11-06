package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

// testAccProtoV6ProviderFactories instantiates the provider for acceptance
// tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"paperclip": providerserver.NewProtocol6WithError(New("test")()),
}

// TestMain points the acceptance tests at an in-memory fake of the Paperclip
// API unless PAPERCLIP_API_URL selects a real instance.
func TestMain(m *testing.M) {
	if os.Getenv(tfresource.EnvTfAcc) != "" && os.Getenv(envEndpoint) == "" {
		server := newFakePaperclip()
		_ = os.Setenv(envEndpoint, server.URL)
		_ = os.Unsetenv(envAPIKey)
		code := m.Run()
		server.Close()
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv(envEndpoint) == "" {
		t.Fatalf("%s must be set for acceptance tests", envEndpoint)
	}
}

// testAccClient returns an API client for checks that go around Terraform.
func testAccClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(client.Config{
		Endpoint: os.Getenv(envEndpoint),
		APIKey:   os.Getenv(envAPIKey),
	})
	if err != nil {
		t.Fatalf("creating API client: %s", err)
	}
	return c
}

// testAccName returns a unique, recognisable name for a test object.
func testAccName() string {
	return "tf-acc-" + acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
}

// testAccCompanyConfig declares a company that allows direct agent creation,
// for use as the parent of other test resources.
func testAccCompanyConfig(name string) string {
	return fmt.Sprintf(`
resource "paperclip_company" "test" {
  name                                  = %[1]q
  require_board_approval_for_new_agents = false
}
`, name)
}

// TestProvider_schemas checks every schema for implementation mistakes, such
// as invalid attribute names or contradictory flags. It needs no API.
func TestProvider_schemas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := New("test")()

	var providerSchema provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &providerSchema)
	if diags := providerSchema.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Errorf("provider schema: %v", diags)
	}

	for _, factory := range p.Resources(ctx) {
		r := factory()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "paperclip"}, &meta)
		var schema resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &schema)
		if diags := schema.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("resource %s schema: %v", meta.TypeName, diags)
		}
		if schema.Schema.MarkdownDescription == "" {
			t.Errorf("resource %s has no description", meta.TypeName)
		}
		for name, attribute := range schema.Schema.Attributes {
			if attribute.GetMarkdownDescription() == "" {
				t.Errorf("resource %s attribute %s has no description", meta.TypeName, name)
			}
		}
	}

	for _, factory := range p.DataSources(ctx) {
		d := factory()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "paperclip"}, &meta)
		var schema datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &schema)
		if diags := schema.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("data source %s schema: %v", meta.TypeName, diags)
		}
		if schema.Schema.MarkdownDescription == "" {
			t.Errorf("data source %s has no description", meta.TypeName)
		}
		for name, attribute := range schema.Schema.Attributes {
			if attribute.GetMarkdownDescription() == "" {
				t.Errorf("data source %s attribute %s has no description", meta.TypeName, name)
			}
		}
	}
}

func TestAccProvider_missingEndpoint(t *testing.T) {
	t.Setenv(envEndpoint, "")

	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config:      `data "paperclip_company" "test" { name = "anything" }`,
				ExpectError: regexp.MustCompile(`Missing Paperclip endpoint`),
			},
		},
	})
}

func TestAccProvider_invalidEndpoint(t *testing.T) {
	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{
				Config: `
provider "paperclip" {
  endpoint = "ftp://paperclip.invalid"
}

data "paperclip_company" "test" { name = "anything" }
`,
				ExpectError: regexp.MustCompile(`must use the http or https\s+scheme`),
			},
		},
	})
}
