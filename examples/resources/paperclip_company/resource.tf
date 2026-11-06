resource "paperclip_company" "studio" {
  name        = "Greenlight Studio"
  description = "Develops, produces and releases short films."

  # A monthly spending limit of 500.00 for the whole company.
  budget_monthly_cents = 50000

  # Required for paperclip_agent, which creates agents without a hire approval.
  require_board_approval_for_new_agents = false
}
