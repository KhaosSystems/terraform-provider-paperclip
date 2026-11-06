package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccAgentResource_basic(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_agent.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Producer"
  adapter_type = "process"
  adapter_config = jsonencode({
    command = "echo"
    args    = ["hello"]
  })
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Producer")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("role"), knownvalue.StringExact("general")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("title"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("reports_to"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adapter_type"), knownvalue.StringExact("process")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("budget_monthly_cents"), knownvalue.Int64Exact(0)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("metadata"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("url_key"), knownvalue.NotNull()),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
				// An import reads the complete settings, including defaults
				// that the configuration does not declare.
				ImportStateVerifyIgnore: []string{"adapter_config", "runtime_config"},
			},
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_agent" "test" {
  company_id           = paperclip_company.test.id
  name                 = "Executive Producer"
  role                 = "pm"
  title                = "Executive Producer"
  capabilities         = "Budgets, schedules and green-lighting."
  adapter_type         = "process"
  budget_monthly_cents = 2500
  adapter_config = jsonencode({
    command = "echo"
  })
  runtime_config = jsonencode({
    heartbeat = { enabled = true, intervalSec = 300 }
  })
  metadata = jsonencode({
    persona = "producer"
  })
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Executive Producer")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("role"), knownvalue.StringExact("pm")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("title"), knownvalue.StringExact("Executive Producer")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("budget_monthly_cents"), knownvalue.Int64Exact(2500)),
					// "args" was removed from the configuration and must be
					// gone, which only works when the config is replaced
					// rather than merged.
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adapter_config"), knownvalue.StringExact(`{"command":"echo"}`)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("metadata"), knownvalue.StringExact(`{"persona":"producer"}`)),
				},
			},
			{
				// Removing optional attributes clears them again.
				Config: testAccCompanyConfig(name) + `
resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Executive Producer"
  adapter_type = "process"
  adapter_config = jsonencode({
    command = "echo"
  })
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("title"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("capabilities"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("metadata"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("budget_monthly_cents"), knownvalue.Int64Exact(0)),
				},
			},
		},
	})
}

// TestAccAgentResource_secretEnv proves that environment values the API
// redacts neither show up as drift nor get overwritten by the placeholder.
func TestAccAgentResource_secretEnv(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_agent.test"

	config := func(title string) string {
		return testAccCompanyConfig(name) + fmt.Sprintf(`
resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Director"
  title        = %[1]q
  adapter_type = "process"
  adapter_config = jsonencode({
    command = "echo"
    env = {
      GREENLIGHT_TOKEN = { type = "plain", value = "s3cret" }
    }
  })
}
`, title)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: config("Director"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adapter_config"), knownvalue.StringExact(
						`{"command":"echo","env":{"GREENLIGHT_TOKEN":{"type":"plain","value":"s3cret"}}}`)),
				},
			},
			{
				// A refresh reads the redacted value and must not plan a change.
				Config: config("Director"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Changing an unrelated attribute must leave the settings alone.
				Config: config("Film Director"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("title"), knownvalue.StringExact("Film Director")),
				},
			},
		},
	})
}

func TestAccAgentResource_reportsTo(t *testing.T) {
	name := testAccName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_agent" "lead" {
  company_id   = paperclip_company.test.id
  name         = "Producer"
  role         = "ceo"
  adapter_type = "process"
}

resource "paperclip_agent" "director" {
  company_id   = paperclip_company.test.id
  name         = "Director"
  reports_to   = paperclip_agent.lead.id
  adapter_type = "process"
}

resource "paperclip_agent" "editor" {
  company_id   = paperclip_company.test.id
  name         = "Editor"
  reports_to   = paperclip_agent.director.id
  adapter_type = "process"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.CompareValuePairs("paperclip_agent.director", tfjsonpath.New("reports_to"),
						"paperclip_agent.lead", tfjsonpath.New("id"), compare.ValuesSame()),
					statecheck.CompareValuePairs("paperclip_agent.editor", tfjsonpath.New("reports_to"),
						"paperclip_agent.director", tfjsonpath.New("id"), compare.ValuesSame()),
				},
			},
		},
	})
}

func TestAccAgentResource_requiresApprovalHint(t *testing.T) {
	name := testAccName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "paperclip_company" "test" {
  name                                  = %[1]q
  require_board_approval_for_new_agents = true
}

resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Producer"
  adapter_type = "process"
}
`, name),
				ExpectError: regexp.MustCompile(`(?s)Unable to create agent.*409`),
			},
		},
	})
}

func TestAccAgentResource_rejectsRedactedPlaceholder(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "paperclip_agent" "test" {
  company_id   = "00000000-0000-4000-8000-000000000000"
  name         = "Producer"
  adapter_type = "process"
  adapter_config = jsonencode({
    env = { TOKEN = "***REDACTED***" }
  })
}
`,
				ExpectError: regexp.MustCompile(`Redacted placeholder in configuration`),
			},
			{
				Config: `
resource "paperclip_agent" "test" {
  company_id     = "00000000-0000-4000-8000-000000000000"
  name           = "Producer"
  adapter_type   = "process"
  adapter_config = jsonencode(["not", "an", "object"])
}
`,
				ExpectError: regexp.MustCompile(`Invalid JSON object`),
			},
		},
	})
}

func TestAccAgentResource_disappears(t *testing.T) {
	name := testAccName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Producer"
  adapter_type = "process"
}
`,
				Check: func(s *terraform.State) error {
					id := s.RootModule().Resources["paperclip_agent.test"].Primary.ID
					return testAccClient(t).DeleteAgent(context.Background(), id)
				},
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
