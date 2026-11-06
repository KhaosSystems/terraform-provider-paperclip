package client

import "context"

// skillEntryFile is the file that holds a skill's main content.
const skillEntryFile = "SKILL.md"

// Skill is a skill in a company's library.
type Skill struct {
	ID               string       `json:"id"`
	CompanyID        string       `json:"companyId"`
	Key              string       `json:"key"`
	Slug             string       `json:"slug"`
	Name             string       `json:"name"`
	Description      *string      `json:"description"`
	Markdown         string       `json:"markdown"`
	SourceType       string       `json:"sourceType"`
	CurrentVersionID *string      `json:"currentVersionId"`
	UsedByAgents     []SkillAgent `json:"usedByAgents"`
}

// SkillAgent is an agent that uses a skill.
type SkillAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SkillCreate is the request body for creating a skill.
type SkillCreate struct {
	Name        string  `json:"name"`
	Slug        *string `json:"slug,omitempty"`
	Description *string `json:"description,omitempty"`
	Markdown    string  `json:"markdown"`
}

// GetSkill returns a single skill.
func (c *Client) GetSkill(ctx context.Context, companyID, id string) (*Skill, error) {
	var out Skill
	if err := c.get(ctx, apiPath("companies", companyID, "skills", id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSkill creates a skill managed by Paperclip.
func (c *Client) CreateSkill(ctx context.Context, companyID string, in SkillCreate) (*Skill, error) {
	var out Skill
	if err := c.post(ctx, apiPath("companies", companyID, "skills"), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RenameSkill changes a skill's display name and keeps its slug, and with it
// the key that agents refer to.
func (c *Client) RenameSkill(ctx context.Context, companyID, id, name, slug string) error {
	body := map[string]any{"name": name, "slug": slug}
	return c.post(ctx, apiPath("companies", companyID, "skills", id, "rename"), body, nil)
}

// UpdateSkillDescription sets a skill's description.
func (c *Client) UpdateSkillDescription(ctx context.Context, companyID, id, description string) error {
	body := map[string]any{"description": description}
	return c.patch(ctx, apiPath("companies", companyID, "skills", id), body, nil)
}

// UpdateSkillMarkdown replaces the content of the skill's SKILL.md.
func (c *Client) UpdateSkillMarkdown(ctx context.Context, companyID, id, markdown string) error {
	body := map[string]any{"path": skillEntryFile, "content": markdown}
	return c.patch(ctx, apiPath("companies", companyID, "skills", id, "files"), body, nil)
}

// DeleteSkill permanently removes a skill. The API refuses while agents still
// use it.
func (c *Client) DeleteSkill(ctx context.Context, companyID, id string) error {
	return c.delete(ctx, apiPath("companies", companyID, "skills", id))
}
