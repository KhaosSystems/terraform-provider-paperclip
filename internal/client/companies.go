package client

import "context"

// Company is a Paperclip company: the top-level container for agents, goals
// and projects.
type Company struct {
	ID                               string  `json:"id"`
	Name                             string  `json:"name"`
	Description                      *string `json:"description"`
	Status                           string  `json:"status"`
	IssuePrefix                      string  `json:"issuePrefix"`
	BudgetMonthlyCents               int64   `json:"budgetMonthlyCents"`
	SpentMonthlyCents                int64   `json:"spentMonthlyCents"`
	RequireBoardApprovalForNewAgents bool    `json:"requireBoardApprovalForNewAgents"`
	CreatedAt                        string  `json:"createdAt"`
	UpdatedAt                        string  `json:"updatedAt"`
}

// CompanyCreate is the request body for creating a company.
type CompanyCreate struct {
	Name               string  `json:"name"`
	Description        *string `json:"description"`
	BudgetMonthlyCents *int64  `json:"budgetMonthlyCents,omitempty"`
}

// CompanyUpdate is the request body for updating a company. Description is
// always sent so that a nil value clears it.
type CompanyUpdate struct {
	Name                             *string `json:"name,omitempty"`
	Description                      *string `json:"description"`
	BudgetMonthlyCents               *int64  `json:"budgetMonthlyCents,omitempty"`
	RequireBoardApprovalForNewAgents *bool   `json:"requireBoardApprovalForNewAgents,omitempty"`
}

// ListCompanies returns every company the caller may see.
func (c *Client) ListCompanies(ctx context.Context) ([]Company, error) {
	var out []Company
	if err := c.get(ctx, apiPath("companies"), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCompany returns a single company.
func (c *Client) GetCompany(ctx context.Context, id string) (*Company, error) {
	var out Company
	if err := c.get(ctx, apiPath("companies", id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateCompany creates a company.
func (c *Client) CreateCompany(ctx context.Context, in CompanyCreate) (*Company, error) {
	var out Company
	if err := c.post(ctx, apiPath("companies"), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateCompany updates a company.
func (c *Client) UpdateCompany(ctx context.Context, id string, in CompanyUpdate) (*Company, error) {
	var out Company
	if err := c.patch(ctx, apiPath("companies", id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCompany permanently removes a company and everything in it.
func (c *Client) DeleteCompany(ctx context.Context, id string) error {
	return c.delete(ctx, apiPath("companies", id))
}
