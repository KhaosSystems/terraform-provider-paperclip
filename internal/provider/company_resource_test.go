package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

func TestAccCompanyResource_basic(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_company.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "paperclip_company" "test" {
  name = %[1]q
}
`, name),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact(name)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("budget_monthly_cents"), knownvalue.Int64Exact(0)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("issue_prefix"), knownvalue.NotNull()),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: fmt.Sprintf(`
resource "paperclip_company" "test" {
  name                                  = "%[1]s renamed"
  description                           = "Makes films with agents."
  budget_monthly_cents                  = 50000
  require_board_approval_for_new_agents = false
}
`, name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact(name+" renamed")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Makes films with agents.")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("budget_monthly_cents"), knownvalue.Int64Exact(50000)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("require_board_approval_for_new_agents"), knownvalue.Bool(false)),
				},
			},
			{
				// Removing optional attributes clears them again.
				Config: fmt.Sprintf(`
resource "paperclip_company" "test" {
  name                                  = "%[1]s renamed"
  require_board_approval_for_new_agents = false
}
`, name),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("budget_monthly_cents"), knownvalue.Int64Exact(0)),
				},
			},
		},
	})
}

func TestAccCompanyResource_approvalPolicyOnCreate(t *testing.T) {
	name := testAccName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("paperclip_company.test",
						tfjsonpath.New("require_board_approval_for_new_agents"), knownvalue.Bool(false)),
				},
			},
		},
	})
}

func TestAccCompanyResource_disappears(t *testing.T) {
	name := testAccName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name),
				Check: func(s *terraform.State) error {
					// Delete the company behind Terraform's back. The next
					// refresh must notice and plan to recreate it.
					id := s.RootModule().Resources["paperclip_company.test"].Primary.ID
					return testAccClient(t).DeleteCompany(context.Background(), id)
				},
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccCompanyResource_validation(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "paperclip_company" "test" {
  name                 = "x"
  budget_monthly_cents = -1
}
`,
				ExpectError: regexp.MustCompile(`must be at least 0`),
			},
		},
	})
}

func testAccCheckCompanyDestroy(t *testing.T) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		c := testAccClient(t)
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "paperclip_company" {
				continue
			}
			_, err := c.GetCompany(context.Background(), rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("company %s still exists", rs.Primary.ID)
			}
			if !client.IsNotFound(err) {
				return fmt.Errorf("checking company %s: %w", rs.Primary.ID, err)
			}
		}
		return nil
	}
}
