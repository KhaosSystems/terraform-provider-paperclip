package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccGoalResource_basic(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_goal.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_goal" "test" {
  company_id = paperclip_company.test.id
  title      = "Release a short film"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("title"), knownvalue.StringExact("Release a short film")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("level"), knownvalue.StringExact("task")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("planned")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("parent_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("owner_agent_id"), knownvalue.Null()),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_agent" "owner" {
  company_id   = paperclip_company.test.id
  name         = "Producer"
  adapter_type = "process"
}

resource "paperclip_goal" "mission" {
  company_id = paperclip_company.test.id
  title      = "Become the best AI film studio"
  level      = "company"
  status     = "active"
}

resource "paperclip_goal" "test" {
  company_id     = paperclip_company.test.id
  title          = "Release a short film this quarter"
  description    = "Ten minutes, festival ready."
  level          = "team"
  status         = "active"
  parent_id      = paperclip_goal.mission.id
  owner_agent_id = paperclip_agent.owner.id
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("title"), knownvalue.StringExact("Release a short film this quarter")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Ten minutes, festival ready.")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("level"), knownvalue.StringExact("team")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("active")),
					statecheck.CompareValuePairs(addr, tfjsonpath.New("parent_id"),
						"paperclip_goal.mission", tfjsonpath.New("id"), compare.ValuesSame()),
					statecheck.CompareValuePairs(addr, tfjsonpath.New("owner_agent_id"),
						"paperclip_agent.owner", tfjsonpath.New("id"), compare.ValuesSame()),
				},
			},
			{
				// Detaching the goal clears the references again.
				Config: testAccCompanyConfig(name) + `
resource "paperclip_goal" "test" {
  company_id = paperclip_company.test.id
  title      = "Release a short film this quarter"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("parent_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("owner_agent_id"), knownvalue.Null()),
				},
			},
		},
	})
}

func TestAccProjectResource_basic(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_project.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_project" "test" {
  company_id = paperclip_company.test.id
  name       = "Short film"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Short film")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("backlog")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("goal_ids"), knownvalue.SetSizeExact(0)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("lead_agent_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("url_key"), knownvalue.NotNull()),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_agent" "lead" {
  company_id   = paperclip_company.test.id
  name         = "Producer"
  adapter_type = "process"
}

resource "paperclip_goal" "test" {
  company_id = paperclip_company.test.id
  title      = "Release a short film"
}

resource "paperclip_project" "test" {
  company_id    = paperclip_company.test.id
  name          = "Short film: pre-production"
  description   = "Script, storyboard and casting."
  status        = "in_progress"
  goal_ids      = [paperclip_goal.test.id]
  lead_agent_id = paperclip_agent.lead.id
  target_date   = "2027-03-31"
  color         = "#ef4444"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Short film: pre-production")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("in_progress")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("goal_ids"), knownvalue.SetSizeExact(1)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("target_date"), knownvalue.StringExact("2027-03-31")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("color"), knownvalue.StringExact("#ef4444")),
					statecheck.CompareValuePairs(addr, tfjsonpath.New("lead_agent_id"),
						"paperclip_agent.lead", tfjsonpath.New("id"), compare.ValuesSame()),
				},
			},
			{
				// Removing the links clears them again.
				Config: testAccCompanyConfig(name) + `
resource "paperclip_project" "test" {
  company_id = paperclip_company.test.id
  name       = "Short film: pre-production"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("goal_ids"), knownvalue.SetSizeExact(0)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("lead_agent_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("target_date"), knownvalue.Null()),
				},
			},
		},
	})
}
