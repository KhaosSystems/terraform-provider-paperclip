package client

import "context"

// Goal is a Paperclip goal. Goals form a tree and may be owned by an agent.
type Goal struct {
	ID           string  `json:"id"`
	CompanyID    string  `json:"companyId"`
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	Level        string  `json:"level"`
	Status       string  `json:"status"`
	ParentID     *string `json:"parentId"`
	OwnerAgentID *string `json:"ownerAgentId"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

// GoalWrite is the request body for creating and updating a goal. Nullable
// fields are always sent so that a nil value clears them.
type GoalWrite struct {
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	Level        *string `json:"level,omitempty"`
	Status       *string `json:"status,omitempty"`
	ParentID     *string `json:"parentId"`
	OwnerAgentID *string `json:"ownerAgentId"`
}

// GetGoal returns a single goal.
func (c *Client) GetGoal(ctx context.Context, id string) (*Goal, error) {
	var out Goal
	if err := c.get(ctx, apiPath("goals", id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateGoal creates a goal in a company.
func (c *Client) CreateGoal(ctx context.Context, companyID string, in GoalWrite) (*Goal, error) {
	var out Goal
	if err := c.post(ctx, apiPath("companies", companyID, "goals"), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateGoal updates a goal.
func (c *Client) UpdateGoal(ctx context.Context, id string, in GoalWrite) (*Goal, error) {
	var out Goal
	if err := c.patch(ctx, apiPath("goals", id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteGoal permanently removes a goal.
func (c *Client) DeleteGoal(ctx context.Context, id string) error {
	return c.delete(ctx, apiPath("goals", id))
}
