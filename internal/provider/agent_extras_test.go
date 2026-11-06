package provider

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/client"
)

func TestAccAgentResource_permissions(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_agent.test"

	config := func(extra string) string {
		return testAccCompanyConfig(name) + fmt.Sprintf(`
resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Producer"
  adapter_type = "process"
  %s
}
`, extra)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: config(`can_create_agents = false`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("can_create_agents"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("can_create_skills"), knownvalue.Bool(true)),
				},
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"adapter_config", "runtime_config"},
			},
			{
				Config: config(`
  can_create_agents = true
  can_create_skills = false
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("can_create_agents"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("can_create_skills"), knownvalue.Bool(false)),
				},
			},
			{
				// Changing an agent must not lose its task assignment grant.
				Config: config(`
  title             = "Producer"
  can_create_agents = false
  can_create_skills = false
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("can_create_agents"), knownvalue.Bool(false)),
				},
				Check: func(s *terraform.State) error {
					agent, err := testAccClient(t).GetAgent(context.Background(), s.RootModule().Resources[addr].Primary.ID)
					if err != nil {
						return err
					}
					if !agent.HasTaskAssignGrant() {
						return fmt.Errorf("the task assignment grant was lost")
					}
					return nil
				},
			},
		},
	})
}

func TestAccAgentResource_instructions(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_agent.test"

	config := func(instructions string) string {
		return testAccCompanyConfig(name) + fmt.Sprintf(`
resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Director"
  adapter_type = "codex_local"
  instructions = %q
}
`, instructions)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: config("# Director\n\nYou direct short films.\n"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("instructions"), knownvalue.StringExact("# Director\n\nYou direct short films.\n")),
				},
				Check: testAccCheckInstructions(t, addr, "# Director\n\nYou direct short films.\n"),
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"adapter_config", "runtime_config", "instructions"},
			},
			{
				Config: config("# Director\n\nYou direct feature films.\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				Check: testAccCheckInstructions(t, addr, "# Director\n\nYou direct feature films.\n"),
			},
			{
				// An edit outside Terraform, for example by the agent itself,
				// shows up as drift.
				Config: config("# Director\n\nYou direct feature films.\n"),
				Check: func(s *terraform.State) error {
					id := s.RootModule().Resources[addr].Primary.ID
					return testAccClient(t).SetAgentInstructions(context.Background(), id, "Edited by hand.")
				},
				ExpectNonEmptyPlan: true,
			},
			{
				// The next apply puts the declared content back.
				Config: config("# Director\n\nYou direct feature films.\n"),
				Check:  testAccCheckInstructions(t, addr, "# Director\n\nYou direct feature films.\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccAgentResource_defaultInstructionsUntouched(t *testing.T) {
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
  name         = "Director"
  adapter_type = "codex_local"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("instructions"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("skills"), knownvalue.Null()),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccAgentResource_instructionsUnsupportedAdapter(t *testing.T) {
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
  name         = "Director"
  adapter_type = "process"
  instructions = "You direct short films."
}
`,
				ExpectError: regexp.MustCompile(`Instructions were not stored`),
			},
		},
	})
}

// TestAccAgentResource_adapterConfigKeepsInstructionsAndSkills guards against a
// real data loss: the API replaces the adapter config wholesale, and that is
// where it records the instruction bundle and the assigned skills.
func TestAccAgentResource_adapterConfigKeepsInstructionsAndSkills(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_agent.test"

	config := func(model string) string {
		return testAccCompanyConfig(name) + fmt.Sprintf(`
resource "paperclip_skill" "test" {
  company_id = paperclip_company.test.id
  name       = "Greenlight API"
  markdown   = "# Greenlight API\n"
}

resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Director"
  adapter_type = "codex_local"
  instructions = "You direct short films."
  skills       = [paperclip_skill.test.key]
  adapter_config = jsonencode({
    model = %q
  })
}
`, model)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{Config: config("one")},
			{
				Config: config("two"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("adapter_config"), knownvalue.StringExact(`{"model":"two"}`)),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(s *terraform.State) error {
					agent, err := testAccClient(t).GetAgent(context.Background(), s.RootModule().Resources[addr].Primary.ID)
					if err != nil {
						return err
					}
					if len(agent.DesiredSkills()) != 1 {
						return fmt.Errorf("skills were lost: %v", agent.AdapterConfig)
					}
					if agent.AdapterConfig["instructionsBundleMode"] != "managed" {
						return fmt.Errorf("the instruction bundle was lost: %v", agent.AdapterConfig)
					}
					return nil
				},
			},
		},
	})
}

func TestAccAgentResource_conflictingAdapterConfig(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "paperclip_agent" "test" {
  company_id   = "00000000-0000-4000-8000-000000000000"
  name         = "Director"
  adapter_type = "codex_local"
  skills       = []
  adapter_config = jsonencode({
    paperclipSkillSync = { desiredSkills = [] }
  })
}
`,
				ExpectError: regexp.MustCompile(`Conflicting skill configuration`),
			},
			{
				Config: `
resource "paperclip_agent" "test" {
  company_id   = "00000000-0000-4000-8000-000000000000"
  name         = "Director"
  adapter_type = "codex_local"
  instructions = "You direct short films."
  adapter_config = jsonencode({
    instructionsEntryFile = "OTHER.md"
  })
}
`,
				ExpectError: regexp.MustCompile(`Conflicting instructions configuration`),
			},
		},
	})
}

func TestAccAgentResource_skills(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_agent.test"

	config := func(body string) string {
		return testAccCompanyConfig(name) + `
resource "paperclip_skill" "one" {
  company_id = paperclip_company.test.id
  name       = "Skill One"
  markdown   = "# One\n"
}

resource "paperclip_skill" "two" {
  company_id = paperclip_company.test.id
  name       = "Skill Two"
  markdown   = "# Two\n"
}
` + fmt.Sprintf(`
resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Director"
  adapter_type = "process"
  %s
}
`, body)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: config(`skills = [paperclip_skill.one.key]`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("skills"), knownvalue.SetSizeExact(1)),
				},
				Check: testAccCheckAgentSkillCount(t, addr, 1),
			},
			{
				Config: config(`skills = [paperclip_skill.one.key, paperclip_skill.two.key]`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("skills"), knownvalue.SetSizeExact(2)),
				},
				Check: testAccCheckAgentSkillCount(t, addr, 2),
			},
			{
				// A skill assigned outside Terraform shows up as drift.
				Config: config(`skills = [paperclip_skill.one.key, paperclip_skill.two.key]`),
				Check: func(s *terraform.State) error {
					id := s.RootModule().Resources[addr].Primary.ID
					key := s.RootModule().Resources["paperclip_skill.one"].Primary.Attributes["key"]
					return testAccClient(t).SyncAgentSkills(context.Background(), id, []string{key})
				},
				ExpectNonEmptyPlan: true,
			},
			{
				// A slug works as well as a key, without showing up as drift.
				Config: config(`skills = ["skill-one"]`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("skills"),
						knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("skill-one")})),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: testAccCheckAgentSkillCount(t, addr, 1),
			},
			{
				// An empty set removes every skill.
				Config: config(`skills = []`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("skills"), knownvalue.SetSizeExact(0)),
				},
				Check: testAccCheckAgentSkillCount(t, addr, 0),
			},
		},
	})
}

func TestAccAgentResource_unknownSkill(t *testing.T) {
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
  name         = "Director"
  adapter_type = "process"
  skills       = ["no-such-skill"]
}
`,
				ExpectError: regexp.MustCompile(`no-such-skill`),
			},
		},
	})
}

func testAccCheckInstructions(t *testing.T, addr, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		got, found, err := testAccClient(t).GetAgentInstructions(context.Background(), s.RootModule().Resources[addr].Primary.ID)
		if err != nil {
			return err
		}
		if !found || got != want {
			return fmt.Errorf("instructions = %q (found %t), want %q", got, found, want)
		}
		return nil
	}
}

func testAccCheckAgentSkillCount(t *testing.T, addr string, want int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		agent, err := testAccClient(t).GetAgent(context.Background(), s.RootModule().Resources[addr].Primary.ID)
		if err != nil {
			return err
		}
		if got := len(agent.DesiredSkills()); got != want {
			return fmt.Errorf("agent has %d skills, want %d", got, want)
		}
		return nil
	}
}

func TestAccSkillResource_basic(t *testing.T) {
	name := testAccName()
	const addr = "paperclip_skill.test"

	config := func(body string) string {
		return testAccCompanyConfig(name) + fmt.Sprintf(`
resource "paperclip_skill" "test" {
  company_id = paperclip_company.test.id
  %s
}
`, body)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: config(`
  name     = "Greenlight API"
  markdown = "---\nname: Greenlight API\ndescription: Calls the Greenlight API.\n---\n\n# Greenlight API\n"
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("slug"), knownvalue.StringExact("greenlight-api")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("key"), knownvalue.StringRegexp(regexp.MustCompile(`^company/.+/greenlight-api$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Calls the Greenlight API.")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("current_version_id"), knownvalue.NotNull()),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources[addr].Primary
					return rs.Attributes["company_id"] + "/" + rs.ID, nil
				},
			},
			{
				// A rename keeps the slug and key, so agents keep their reference.
				Config: config(`
  name     = "Greenlight"
  markdown = "---\nname: Greenlight\ndescription: Calls the Greenlight API.\n---\n\n# Greenlight, updated\n"
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Greenlight")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("slug"), knownvalue.StringExact("greenlight-api")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("markdown"), knownvalue.StringRegexp(regexp.MustCompile(`updated`))),
				},
			},
			{
				// Without frontmatter the description attribute is used.
				Config: config(`
  name        = "Greenlight"
  description = "Talks to Greenlight."
  markdown    = "# Greenlight\n"
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Talks to Greenlight.")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("markdown"), knownvalue.StringExact("# Greenlight\n")),
				},
			},
		},
	})
}

func TestAccSkillResource_validation(t *testing.T) {
	config := func(body string) string {
		return fmt.Sprintf(`
resource "paperclip_skill" "test" {
  company_id = "00000000-0000-4000-8000-000000000000"
  %s
}
`, body)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(`
  name     = "Declared"
  markdown = "---\nname: Different\n---\n"
`),
				ExpectError: regexp.MustCompile(`differs from the skill's frontmatter`),
			},
			{
				Config: config(`
  name        = "Declared"
  description = "Declared."
  markdown    = "---\ndescription: Different.\n---\n"
`),
				ExpectError: regexp.MustCompile(`differs from the skill's frontmatter`),
			},
			{
				Config: config(`
  name     = "Declared"
  markdown = "   "
`),
				ExpectError: regexp.MustCompile(`must not be blank`),
			},
			{
				Config: config(`
  name     = "Declared"
  slug     = "Not A Slug"
  markdown = "# Declared\n"
`),
				ExpectError: regexp.MustCompile(`lowercase letters`),
			},
		},
	})
}

// TestAccSkillResource_detachAndDelete removes a skill and its assignment in one
// apply. Terraform deletes the skill before updating the agent, so the delete has
// to detach the skill first. A second agent that keeps another skill proves the
// detach leaves other assignments alone.
func TestAccSkillResource_detachAndDelete(t *testing.T) {
	name := testAccName()

	keep := `
resource "paperclip_skill" "keep" {
  company_id = paperclip_company.test.id
  name       = "Keep"
  markdown   = "# Keep\n"
}
`
	withSkill := testAccCompanyConfig(name) + keep + `
resource "paperclip_skill" "test" {
  company_id = paperclip_company.test.id
  name       = "Greenlight API"
  markdown   = "# Greenlight API\n"
}

resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Director"
  adapter_type = "process"
  skills       = [paperclip_skill.test.key]
}

resource "paperclip_agent" "other" {
  company_id   = paperclip_company.test.id
  name         = "Editor"
  adapter_type = "process"
  skills       = [paperclip_skill.test.key, paperclip_skill.keep.key]
}
`
	withoutSkill := testAccCompanyConfig(name) + keep + `
resource "paperclip_agent" "other" {
  company_id   = paperclip_company.test.id
  name         = "Editor"
  adapter_type = "process"
  skills       = [paperclip_skill.keep.key]
}

resource "paperclip_agent" "test" {
  company_id   = paperclip_company.test.id
  name         = "Director"
  adapter_type = "process"
  skills       = []
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{Config: withSkill},
			{
				Config: withoutSkill,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckAgentSkillCount(t, "paperclip_agent.test", 0),
					testAccCheckAgentSkillCount(t, "paperclip_agent.other", 1),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestAccSkillResource_invalidImportID(t *testing.T) {
	name := testAccName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCompanyDestroy(t),
		Steps: []resource.TestStep{
			{
				Config: testAccCompanyConfig(name) + `
resource "paperclip_skill" "test" {
  company_id = paperclip_company.test.id
  name       = "Greenlight API"
  markdown   = "# Greenlight API\n"
}
`,
			},
			{
				ResourceName:  "paperclip_skill.test",
				ImportState:   true,
				ImportStateId: "missing-a-separator",
				ExpectError:   regexp.MustCompile(`Expected company_id/skill_id`),
			},
		},
	})
}

func TestWithServerManagedKeys(t *testing.T) {
	t.Parallel()

	remote := map[string]any{
		"command":                "old",
		"timeoutSec":             float64(10),
		"instructionsBundleMode": "managed",
		"paperclipSkillSync":     map[string]any{"desiredSkills": []any{"a"}},
	}

	got := withServerManagedKeys(map[string]any{"command": "new"}, remote)
	want := map[string]any{
		"command":                "new",
		"instructionsBundleMode": "managed",
		"paperclipSkillSync":     map[string]any{"desiredSkills": []any{"a"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("withServerManagedKeys() = %v, want %v", got, want)
	}

	// A declared value wins, and nil inputs are safe.
	got = withServerManagedKeys(map[string]any{"instructionsBundleMode": "external"}, remote)
	if got["instructionsBundleMode"] != "external" {
		t.Errorf("declared value was overwritten: %v", got)
	}
	if got := withServerManagedKeys(nil, nil); len(got) != 0 {
		t.Errorf("withServerManagedKeys(nil, nil) = %v, want empty", got)
	}

	stripped := withoutServerManagedKeys(remote)
	if !reflect.DeepEqual(stripped, map[string]any{"command": "old", "timeoutSec": float64(10)}) {
		t.Errorf("withoutServerManagedKeys() = %v", stripped)
	}
	if _, ok := remote["paperclipSkillSync"]; !ok {
		t.Error("withoutServerManagedKeys() must not change its input")
	}
}

func TestSkillReferences(t *testing.T) {
	t.Parallel()

	const key = "company/c1/greenlight-api"

	tests := map[string]struct {
		declared, remote, want []string
		equal                  bool
	}{
		"same key":         {[]string{key}, []string{key}, []string{key}, true},
		"slug matches":     {[]string{"greenlight-api"}, []string{key}, []string{"greenlight-api"}, true},
		"case differs":     {[]string{"Greenlight-API"}, []string{key}, []string{"Greenlight-API"}, true},
		"partial slug":     {[]string{"api"}, []string{key}, []string{key}, false},
		"extra remote":     {[]string{key}, []string{key, "company/c1/other"}, []string{key, "company/c1/other"}, false},
		"missing remote":   {[]string{key}, nil, []string{}, false},
		"both empty":       {nil, nil, []string{}, true},
		"order is ignored": {[]string{"b/x", "a/y"}, []string{"a/y", "b/x"}, []string{"a/y", "b/x"}, true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := matchSkillReferences(tc.declared, tc.remote); !slices.Equal(got, tc.want) {
				t.Errorf("matchSkillReferences() = %v, want %v", got, tc.want)
			}
			if got := skillSetsEqual(tc.declared, tc.remote); got != tc.equal {
				t.Errorf("skillSetsEqual() = %t, want %t", got, tc.equal)
			}
		})
	}
}

func TestFrontmatterValue(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		markdown, key, want string
		found               bool
	}{
		"plain":            {"---\nname: Greenlight\n---\n", "name", "Greenlight", true},
		"double quoted":    {"---\nname: \"Green: light\"\n---\n", "name", "Green: light", true},
		"single quoted":    {"---\nname: 'Greenlight'\n---\n", "name", "Greenlight", true},
		"windows newlines": {"---\r\nname: Greenlight\r\n---\r\n", "name", "Greenlight", true},
		"no frontmatter":   {"# name: Greenlight\n", "name", "", false},
		"missing key":      {"---\nother: x\n---\n", "name", "", false},
		"after the block":  {"---\nother: x\n---\nname: Greenlight\n", "name", "", false},
		"nested key":       {"---\nmeta:\n  name: Nested\n---\n", "name", "", false},
		"block scalar":     {"---\nname: >\n  Greenlight\n---\n", "name", "", false},
		"empty value":      {"---\nname:\n---\n", "name", "", false},
		"empty document":   {"", "name", "", false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, found := frontmatterValue(tc.markdown, tc.key)
			if got != tc.want || found != tc.found {
				t.Errorf("frontmatterValue() = %q, %t, want %q, %t", got, found, tc.want, tc.found)
			}
		})
	}
}

func TestPermissionsUpdate(t *testing.T) {
	t.Parallel()

	yes, no := true, false
	withGrant := &client.AgentAccess{Grants: []client.AgentGrant{{PermissionKey: "tasks:assign"}}}

	tests := map[string]struct {
		plan        agentResourceModel
		current     client.Agent
		wantChanged bool
		want        client.AgentPermissionsUpdate
	}{
		"nothing declared": {
			current: client.Agent{Permissions: &client.AgentPermissions{CanCreateAgents: true}, Access: withGrant},
			want:    client.AgentPermissionsUpdate{CanCreateAgents: true, CanAssignTasks: true},
		},
		"hiring turned off keeps the task grant": {
			plan:        agentResourceModel{CanCreateAgents: boolValue(false)},
			current:     client.Agent{Permissions: &client.AgentPermissions{CanCreateAgents: true}, Access: withGrant},
			wantChanged: true,
			want:        client.AgentPermissionsUpdate{CanCreateAgents: false, CanAssignTasks: true},
		},
		"no explicit grant stays that way": {
			plan:        agentResourceModel{CanCreateAgents: boolValue(true)},
			current:     client.Agent{Permissions: &client.AgentPermissions{CanCreateAgents: false}},
			wantChanged: true,
			want:        client.AgentPermissionsUpdate{CanCreateAgents: true, CanAssignTasks: false},
		},
		"already as declared": {
			plan:    agentResourceModel{CanCreateAgents: boolValue(false), CanCreateSkills: boolValue(true)},
			current: client.Agent{Permissions: &client.AgentPermissions{CanCreateAgents: false, CanCreateSkills: &yes}},
			want:    client.AgentPermissionsUpdate{CanCreateAgents: false, CanCreateSkills: &yes},
		},
		"skills permission changed": {
			plan:        agentResourceModel{CanCreateSkills: boolValue(false)},
			current:     client.Agent{Permissions: &client.AgentPermissions{CanCreateSkills: &yes}},
			wantChanged: true,
			want:        client.AgentPermissionsUpdate{CanCreateSkills: &no},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, changed := permissionsUpdate(&tc.plan, &tc.current)
			if changed != tc.wantChanged {
				t.Errorf("changed = %t, want %t", changed, tc.wantChanged)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("update = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func boolValue(v bool) types.Bool {
	return types.BoolValue(v)
}
