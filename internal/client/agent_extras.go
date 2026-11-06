package client

import (
	"context"
	"net/http"
	"net/url"
)

// AgentPermissions holds the permissions stored on an agent.
type AgentPermissions struct {
	CanCreateAgents bool  `json:"canCreateAgents"`
	CanCreateSkills *bool `json:"canCreateSkills"`
}

// AgentPermissionsCreate is the optional permissions block when creating an
// agent. Whatever is left out gets the server's default.
type AgentPermissionsCreate struct {
	CanCreateAgents *bool `json:"canCreateAgents,omitempty"`
	CanCreateSkills *bool `json:"canCreateSkills,omitempty"`
}

// AgentPermissionsUpdate is the request body for updating permissions. The API
// requires CanCreateAgents and CanAssignTasks on every call.
type AgentPermissionsUpdate struct {
	CanCreateAgents bool  `json:"canCreateAgents"`
	CanCreateSkills *bool `json:"canCreateSkills,omitempty"`
	CanAssignTasks  bool  `json:"canAssignTasks"`
}

// AgentAccess describes an agent's access state. Only the single-agent read
// returns it.
type AgentAccess struct {
	Grants []AgentGrant `json:"grants"`
}

// AgentGrant is a permission granted directly to an agent.
type AgentGrant struct {
	PermissionKey string `json:"permissionKey"`
}

// AgentInstructionsBundleCreate seeds an agent's instruction files on creation.
type AgentInstructionsBundleCreate struct {
	EntryFile string            `json:"entryFile,omitempty"`
	Files     map[string]string `json:"files"`
}

// DesiredSkills returns the skill keys assigned to the agent. The server keeps
// them inside the adapter config, as plain strings or as objects with a key.
func (a *Agent) DesiredSkills() []string {
	sync, ok := a.AdapterConfig["paperclipSkillSync"].(map[string]any)
	if !ok {
		return nil
	}
	values, ok := sync["desiredSkills"].([]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(values))
	for _, value := range values {
		switch v := value.(type) {
		case string:
			keys = append(keys, v)
		case map[string]any:
			if key, ok := v["key"].(string); ok {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// HasTaskAssignGrant reports whether the agent holds an explicit task
// assignment grant.
func (a *Agent) HasTaskAssignGrant() bool {
	if a.Access == nil {
		return false
	}
	for _, grant := range a.Access.Grants {
		if grant.PermissionKey == "tasks:assign" {
			return true
		}
	}
	return false
}

// UpdateAgentPermissions updates an agent's permissions.
func (c *Client) UpdateAgentPermissions(ctx context.Context, id string, in AgentPermissionsUpdate) error {
	return c.patch(ctx, apiPath("agents", id, "permissions"), in, nil)
}

// SyncAgentSkills replaces the complete set of skills assigned to an agent.
func (c *Client) SyncAgentSkills(ctx context.Context, id string, skills []string) error {
	if skills == nil {
		skills = []string{}
	}
	body := map[string]any{"mode": "replace", "desiredSkills": skills}
	return c.post(ctx, apiPath("agents", id, "skills", "sync"), body, nil)
}

// DetachSkill removes one skill from an agent and leaves its other skills alone.
func (c *Client) DetachSkill(ctx context.Context, agentID, skillKey string) error {
	body := map[string]any{"mode": "remove", "desiredSkills": []string{skillKey}}
	return c.post(ctx, apiPath("agents", agentID, "skills", "sync"), body, nil)
}

type instructionsBundle struct {
	EntryFile string `json:"entryFile"`
}

type instructionsFile struct {
	Content     string `json:"content"`
	ContentHash string `json:"contentHash"`
	Revision    *struct {
		ID string `json:"id"`
	} `json:"revision"`
}

// readInstructionsEntry returns the name of the entry file and its current
// content, or a nil file when the entry does not exist yet.
func (c *Client) readInstructionsEntry(ctx context.Context, agentID string) (string, *instructionsFile, error) {
	var bundle instructionsBundle
	if err := c.get(ctx, apiPath("agents", agentID, "instructions-bundle"), &bundle); err != nil {
		return "", nil, err
	}

	var file instructionsFile
	path := apiPath("agents", agentID, "instructions-bundle", "file") + "?path=" + url.QueryEscape(bundle.EntryFile)
	if err := c.get(ctx, path, &file); err != nil {
		if IsNotFound(err) {
			return bundle.EntryFile, nil, nil
		}
		return "", nil, err
	}
	return bundle.EntryFile, &file, nil
}

// GetAgentInstructions returns the content of the agent's instruction entry
// file. The boolean is false when no entry file exists.
func (c *Client) GetAgentInstructions(ctx context.Context, agentID string) (string, bool, error) {
	_, file, err := c.readInstructionsEntry(ctx, agentID)
	if err != nil || file == nil {
		return "", false, err
	}
	return file.Content, true, nil
}

// SetAgentInstructions writes the agent's instruction entry file.
//
// The API rejects a write that does not name the revision it is based on, so
// this reads the entry first. A change made between the read and the write
// fails with a 409 rather than being overwritten.
func (c *Client) SetAgentInstructions(ctx context.Context, agentID, content string) error {
	entry, current, err := c.readInstructionsEntry(ctx, agentID)
	if err != nil {
		return err
	}
	if current != nil && current.Content == content {
		return nil
	}

	body := map[string]any{"path": entry, "content": content}
	switch {
	case current == nil:
		body["baseRevisionId"] = nil
	case current.Revision != nil:
		body["baseRevisionId"] = current.Revision.ID
	case current.ContentHash != "":
		body["baseHash"] = current.ContentHash
	default:
		body["baseRevisionId"] = nil
	}
	return c.do(ctx, http.MethodPut, apiPath("agents", agentID, "instructions-bundle", "file"), body, nil)
}
