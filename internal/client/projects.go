package client

import "context"

// Project is a Paperclip project: a body of work that groups tasks and links
// them to goals.
type Project struct {
	ID          string   `json:"id"`
	CompanyID   string   `json:"companyId"`
	URLKey      string   `json:"urlKey"`
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	Status      string   `json:"status"`
	GoalIDs     []string `json:"goalIds"`
	LeadAgentID *string  `json:"leadAgentId"`
	TargetDate  *string  `json:"targetDate"`
	Color       *string  `json:"color"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// ProjectWrite is the request body for creating and updating a project.
// Nullable fields and GoalIDs are always sent so that the server state
// converges on exactly what is declared.
type ProjectWrite struct {
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	Status      *string  `json:"status,omitempty"`
	GoalIDs     []string `json:"goalIds"`
	LeadAgentID *string  `json:"leadAgentId"`
	TargetDate  *string  `json:"targetDate"`
	Color       *string  `json:"color,omitempty"`
}

// GetProject returns a single project.
func (c *Client) GetProject(ctx context.Context, id string) (*Project, error) {
	var out Project
	if err := c.get(ctx, apiPath("projects", id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateProject creates a project in a company.
func (c *Client) CreateProject(ctx context.Context, companyID string, in ProjectWrite) (*Project, error) {
	if in.GoalIDs == nil {
		in.GoalIDs = []string{}
	}
	var out Project
	if err := c.post(ctx, apiPath("companies", companyID, "projects"), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateProject updates a project.
func (c *Client) UpdateProject(ctx context.Context, id string, in ProjectWrite) (*Project, error) {
	if in.GoalIDs == nil {
		in.GoalIDs = []string{}
	}
	var out Project
	if err := c.patch(ctx, apiPath("projects", id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProject permanently removes a project.
func (c *Client) DeleteProject(ctx context.Context, id string) error {
	return c.delete(ctx, apiPath("projects", id))
}
