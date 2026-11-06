package client

import "context"

// RedactedValue is the placeholder the API returns in place of sensitive
// values, such as plain adapter environment variables.
const RedactedValue = "***REDACTED***"

// Agent is a Paperclip agent: an AI employee of a company.
type Agent struct {
	ID                 string            `json:"id"`
	CompanyID          string            `json:"companyId"`
	Name               string            `json:"name"`
	URLKey             string            `json:"urlKey"`
	Role               string            `json:"role"`
	Title              *string           `json:"title"`
	Status             string            `json:"status"`
	ReportsTo          *string           `json:"reportsTo"`
	Capabilities       *string           `json:"capabilities"`
	AdapterType        string            `json:"adapterType"`
	AdapterConfig      map[string]any    `json:"adapterConfig"`
	RuntimeConfig      map[string]any    `json:"runtimeConfig"`
	BudgetMonthlyCents int64             `json:"budgetMonthlyCents"`
	SpentMonthlyCents  int64             `json:"spentMonthlyCents"`
	Metadata           map[string]any    `json:"metadata"`
	Permissions        *AgentPermissions `json:"permissions"`
	Access             *AgentAccess      `json:"access"`
	CreatedAt          string            `json:"createdAt"`
	UpdatedAt          string            `json:"updatedAt"`
}

// AgentCreate is the request body for creating an agent.
type AgentCreate struct {
	Name               string                  `json:"name"`
	Role               *string                 `json:"role,omitempty"`
	Title              *string                 `json:"title"`
	ReportsTo          *string                 `json:"reportsTo"`
	Capabilities       *string                 `json:"capabilities"`
	AdapterType        string                  `json:"adapterType"`
	AdapterConfig      map[string]any          `json:"adapterConfig,omitzero"`
	RuntimeConfig      map[string]any          `json:"runtimeConfig,omitzero"`
	BudgetMonthlyCents *int64                  `json:"budgetMonthlyCents,omitempty"`
	Metadata           map[string]any          `json:"metadata"`
	Permissions        *AgentPermissionsCreate `json:"permissions,omitempty"`
	// DesiredSkills is sent when non-nil, so an empty list assigns no skills.
	DesiredSkills      []string                       `json:"desiredSkills,omitzero"`
	InstructionsBundle *AgentInstructionsBundleCreate `json:"instructionsBundle,omitempty"`
}

// AgentUpdate is the request body for updating an agent. Nullable fields are
// always sent so that a nil value clears them.
type AgentUpdate struct {
	Name               *string        `json:"name,omitempty"`
	Role               *string        `json:"role,omitempty"`
	Title              *string        `json:"title"`
	ReportsTo          *string        `json:"reportsTo"`
	Capabilities       *string        `json:"capabilities"`
	AdapterType        *string        `json:"adapterType,omitempty"`
	AdapterConfig      map[string]any `json:"adapterConfig,omitzero"`
	RuntimeConfig      map[string]any `json:"runtimeConfig,omitzero"`
	BudgetMonthlyCents *int64         `json:"budgetMonthlyCents,omitempty"`
	Metadata           map[string]any `json:"metadata"`
	// ReplaceAdapterConfig makes the server replace the adapter config
	// instead of merging it, which is what lets removed keys disappear.
	ReplaceAdapterConfig bool `json:"replaceAdapterConfig,omitempty"`
}

// ListAgents returns the agents of a company.
func (c *Client) ListAgents(ctx context.Context, companyID string) ([]Agent, error) {
	var out []Agent
	if err := c.get(ctx, apiPath("companies", companyID, "agents"), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAgent returns a single agent.
func (c *Client) GetAgent(ctx context.Context, id string) (*Agent, error) {
	var out Agent
	if err := c.get(ctx, apiPath("agents", id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateAgent creates an agent in a company.
func (c *Client) CreateAgent(ctx context.Context, companyID string, in AgentCreate) (*Agent, error) {
	var out Agent
	if err := c.post(ctx, apiPath("companies", companyID, "agents"), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAgent updates an agent.
func (c *Client) UpdateAgent(ctx context.Context, id string, in AgentUpdate) (*Agent, error) {
	var out Agent
	if err := c.patch(ctx, apiPath("agents", id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgent permanently removes an agent.
func (c *Client) DeleteAgent(ctx context.Context, id string) error {
	return c.delete(ctx, apiPath("agents", id))
}
