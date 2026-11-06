package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const testAccDataSourcesFixture = `
resource "paperclip_agent" "producer" {
  company_id   = paperclip_company.test.id
  name         = "Producer"
  role         = "ceo"
  adapter_type = "process"
}

resource "paperclip_agent" "director" {
  company_id   = paperclip_company.test.id
  name         = "Director"
  title        = "Film Director"
  reports_to   = paperclip_agent.producer.id
  adapter_type = "process"
}
`

func TestAccCompanyDataSource(t *testing.T) {
	name := testAccName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name) + `
data "paperclip_company" "missing" {
  name       = "tf-acc-this-company-does-not-exist"
  depends_on = [paperclip_company.test]
}
`,
				ExpectError: regexp.MustCompile(`Company not found`),
			},
			{
				Config: testAccCompanyConfig(name) + `
data "paperclip_company" "invalid" {
  id   = paperclip_company.test.id
  name = "both"
}
`,
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination`),
			},
			{
				Config: testAccCompanyConfig(name) + `
data "paperclip_company" "by_id" {
  id = paperclip_company.test.id
}

data "paperclip_company" "by_name" {
  name       = paperclip_company.test.name
  depends_on = [paperclip_company.test]
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.paperclip_company.by_id", tfjsonpath.New("name"), knownvalue.StringExact(name)),
					statecheck.ExpectKnownValue("data.paperclip_company.by_id", tfjsonpath.New("status"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue("data.paperclip_company.by_id",
						tfjsonpath.New("require_board_approval_for_new_agents"), knownvalue.Bool(false)),
					statecheck.CompareValuePairs("data.paperclip_company.by_name", tfjsonpath.New("id"),
						"paperclip_company.test", tfjsonpath.New("id"), compare.ValuesSame()),
				},
			},
		},
	})
}

func TestAccAgentDataSources(t *testing.T) {
	name := testAccName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name) + testAccDataSourcesFixture + `
data "paperclip_agent" "missing" {
  company_id = paperclip_company.test.id
  name       = "Nobody"
  depends_on = [paperclip_agent.producer, paperclip_agent.director]
}
`,
				ExpectError: regexp.MustCompile(`Agent not found`),
			},
			{
				Config: testAccCompanyConfig(name) + testAccDataSourcesFixture + `
data "paperclip_agent" "invalid" {
  name = "Producer"
}
`,
				ExpectError: regexp.MustCompile(`(?s)Invalid Attribute Combination`),
			},
			{
				Config: testAccCompanyConfig(name) + testAccDataSourcesFixture + `
data "paperclip_agent" "by_id" {
  id = paperclip_agent.director.id
}

data "paperclip_agent" "by_name" {
  company_id = paperclip_company.test.id
  name       = paperclip_agent.producer.name
}

data "paperclip_agents" "all" {
  company_id = paperclip_company.test.id
  depends_on = [paperclip_agent.producer, paperclip_agent.director]
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.paperclip_agent.by_id", tfjsonpath.New("name"), knownvalue.StringExact("Director")),
					statecheck.ExpectKnownValue("data.paperclip_agent.by_id", tfjsonpath.New("title"), knownvalue.StringExact("Film Director")),
					statecheck.ExpectKnownValue("data.paperclip_agent.by_id", tfjsonpath.New("adapter_type"), knownvalue.StringExact("process")),
					statecheck.CompareValuePairs("data.paperclip_agent.by_id", tfjsonpath.New("reports_to"),
						"paperclip_agent.producer", tfjsonpath.New("id"), compare.ValuesSame()),
					statecheck.CompareValuePairs("data.paperclip_agent.by_name", tfjsonpath.New("id"),
						"paperclip_agent.producer", tfjsonpath.New("id"), compare.ValuesSame()),
					statecheck.ExpectKnownValue("data.paperclip_agent.by_name", tfjsonpath.New("role"), knownvalue.StringExact("ceo")),
					// Ordered by name: Director, then Producer.
					statecheck.ExpectKnownValue("data.paperclip_agents.all", tfjsonpath.New("agents"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue("data.paperclip_agents.all",
						tfjsonpath.New("agents").AtSliceIndex(0).AtMapKey("name"), knownvalue.StringExact("Director")),
					statecheck.ExpectKnownValue("data.paperclip_agents.all",
						tfjsonpath.New("agents").AtSliceIndex(1).AtMapKey("name"), knownvalue.StringExact("Producer")),
				},
			},
		},
	})
}
