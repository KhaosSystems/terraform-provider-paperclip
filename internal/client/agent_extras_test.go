package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestAgentDesiredSkills(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config string
		want   []string
	}{
		"no config":   {`{}`, nil},
		"wrong shape": {`{"paperclipSkillSync":"x"}`, nil},
		"no list":     {`{"paperclipSkillSync":{}}`, nil},
		"empty":       {`{"paperclipSkillSync":{"desiredSkills":[]}}`, []string{}},
		"strings":     {`{"paperclipSkillSync":{"desiredSkills":["a","b"]}}`, []string{"a", "b"}},
		"objects":     {`{"paperclipSkillSync":{"desiredSkills":[{"key":"a","versionId":null},"b"]}}`, []string{"a", "b"}},
		"bad entries": {`{"paperclipSkillSync":{"desiredSkills":[1,{"versionId":"x"},"a"]}}`, []string{"a"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var agent Agent
			if err := json.Unmarshal([]byte(`{"adapterConfig":`+tc.config+`}`), &agent); err != nil {
				t.Fatal(err)
			}
			if got := agent.DesiredSkills(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DesiredSkills() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestAgentHasTaskAssignGrant(t *testing.T) {
	t.Parallel()

	if (&Agent{}).HasTaskAssignGrant() {
		t.Error("an agent without access state has no grant")
	}
	other := &Agent{Access: &AgentAccess{Grants: []AgentGrant{{PermissionKey: "tools:use"}}}}
	if other.HasTaskAssignGrant() {
		t.Error("another grant must not count")
	}
	has := &Agent{Access: &AgentAccess{Grants: []AgentGrant{{PermissionKey: "tools:use"}, {PermissionKey: "tasks:assign"}}}}
	if !has.HasTaskAssignGrant() {
		t.Error("the grant was not found")
	}
}

func TestAgentCreate_extras(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, calls := recordingClient(t, `{"id":"a1"}`)

	no := false
	if _, err := c.CreateAgent(ctx, "c1", AgentCreate{
		Name: "Director", AdapterType: "codex_local",
		Permissions:        &AgentPermissionsCreate{CanCreateAgents: &no},
		DesiredSkills:      []string{},
		InstructionsBundle: &AgentInstructionsBundleCreate{EntryFile: "AGENTS.md", Files: map[string]string{"AGENTS.md": "Hello"}},
	}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPost, "/api/companies/c1/agents",
		`{"name":"Director","title":null,"reportsTo":null,"capabilities":null,"adapterType":"codex_local","metadata":null,
		  "permissions":{"canCreateAgents":false},"desiredSkills":[],
		  "instructionsBundle":{"entryFile":"AGENTS.md","files":{"AGENTS.md":"Hello"}}}`)
}

func TestAgentPermissionsAndSkillEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, calls := recordingClient(t, `{}`)

	if err := c.UpdateAgentPermissions(ctx, "a1", AgentPermissionsUpdate{CanCreateAgents: false, CanAssignTasks: true}); err != nil {
		t.Fatal(err)
	}
	// Both required booleans are sent even when false.
	assertCall(t, calls, http.MethodPatch, "/api/agents/a1/permissions", `{"canCreateAgents":false,"canAssignTasks":true}`)

	if err := c.SyncAgentSkills(ctx, "a1", nil); err != nil {
		t.Fatal(err)
	}
	// An empty list, not null, and always a full replacement.
	assertCall(t, calls, http.MethodPost, "/api/agents/a1/skills/sync", `{"mode":"replace","desiredSkills":[]}`)
}

// instructionsServer answers the bundle and file reads and records the write.
func instructionsServer(t *testing.T, entry string, fileStatus int, file string) (*Client, *string) {
	t.Helper()
	var written string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/instructions-bundle"):
			_, _ = w.Write([]byte(`{"entryFile":"` + entry + `"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/instructions-bundle/file"):
			if got := r.URL.Query().Get("path"); got != entry {
				t.Errorf("read path = %q, want %q", got, entry)
			}
			w.WriteHeader(fileStatus)
			_, _ = w.Write([]byte(file))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/instructions-bundle/file"):
			body, _ := io.ReadAll(r.Body)
			written = string(body)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	return c, &written
}

func TestGetAgentInstructions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	c, _ := instructionsServer(t, "AGENTS.md", http.StatusOK, `{"content":"Hello"}`)
	content, found, err := c.GetAgentInstructions(ctx, "a1")
	if err != nil || !found || content != "Hello" {
		t.Errorf("GetAgentInstructions() = %q, %t, %v", content, found, err)
	}

	c, _ = instructionsServer(t, "AGENTS.md", http.StatusNotFound, `{"error":"File not found"}`)
	if content, found, err := c.GetAgentInstructions(ctx, "a1"); err != nil || found || content != "" {
		t.Errorf("missing file: GetAgentInstructions() = %q, %t, %v", content, found, err)
	}

	c, _ = instructionsServer(t, "AGENTS.md", http.StatusForbidden, `{"error":"Forbidden"}`)
	if _, _, err := c.GetAgentInstructions(ctx, "a1"); err == nil {
		t.Error("other errors must be returned")
	}

	// A custom entry file name is used and escaped.
	c, _ = instructionsServer(t, "my file.md", http.StatusOK, `{"content":"Hello"}`)
	if _, found, err := c.GetAgentInstructions(ctx, "a1"); err != nil || !found {
		t.Errorf("custom entry: found %t, error %v", found, err)
	}
}

func TestSetAgentInstructions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status int
		file   string
		want   string
	}{
		"existing revision": {
			http.StatusOK, `{"content":"Old","revision":{"id":"r1"}}`,
			`{"baseRevisionId":"r1","content":"New","path":"AGENTS.md"}`,
		},
		"file without a revision uses its hash": {
			http.StatusOK, `{"content":"Old","contentHash":"abc"}`,
			`{"baseHash":"abc","content":"New","path":"AGENTS.md"}`,
		},
		"new entry sends an explicit null": {
			http.StatusNotFound, `{"error":"File not found"}`,
			`{"baseRevisionId":null,"content":"New","path":"AGENTS.md"}`,
		},
		"identical content is not written": {
			http.StatusOK, `{"content":"New","revision":{"id":"r1"}}`,
			``,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, written := instructionsServer(t, "AGENTS.md", tc.status, tc.file)
			if err := c.SetAgentInstructions(context.Background(), "a1", "New"); err != nil {
				t.Fatal(err)
			}
			if *written != tc.want {
				t.Errorf("body = %s, want %s", *written, tc.want)
			}
		})
	}
}

func TestSkillEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	c, calls := recordingClient(t, `{"id":"s1","companyId":"c1","key":"company/c1/film","slug":"film","name":"Film",
		"description":null,"markdown":"# Film\n","sourceType":"managed","currentVersionId":"v1"}`)

	skill, err := c.GetSkill(ctx, "c1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodGet, "/api/companies/c1/skills/s1", "")
	if skill.Key != "company/c1/film" || skill.Slug != "film" || skill.Markdown != "# Film\n" ||
		skill.Description != nil || skill.CurrentVersionID == nil || *skill.CurrentVersionID != "v1" {
		t.Errorf("unexpected skill: %+v", skill)
	}

	if _, err := c.CreateSkill(ctx, "c1", SkillCreate{Name: "Film", Markdown: "# Film\n"}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPost, "/api/companies/c1/skills", `{"name":"Film","markdown":"# Film\n"}`)

	slug, description := "film", "About film."
	if _, err := c.CreateSkill(ctx, "c1", SkillCreate{Name: "Film", Slug: &slug, Description: &description, Markdown: "x"}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPost, "/api/companies/c1/skills",
		`{"name":"Film","slug":"film","description":"About film.","markdown":"x"}`)

	if err := c.RenameSkill(ctx, "c1", "s1", "Cinema", "film"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPost, "/api/companies/c1/skills/s1/rename", `{"name":"Cinema","slug":"film"}`)

	if err := c.UpdateSkillDescription(ctx, "c1", "s1", "New."); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPatch, "/api/companies/c1/skills/s1", `{"description":"New."}`)

	if err := c.UpdateSkillMarkdown(ctx, "c1", "s1", "# New\n"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPatch, "/api/companies/c1/skills/s1/files", `{"path":"SKILL.md","content":"# New\n"}`)

	if err := c.DetachSkill(ctx, "a1", "company/c1/film"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPost, "/api/agents/a1/skills/sync", `{"mode":"remove","desiredSkills":["company/c1/film"]}`)

	if err := c.DeleteSkill(ctx, "c1", "s1"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodDelete, "/api/companies/c1/skills/s1", "")
}
