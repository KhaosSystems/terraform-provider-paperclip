# Look an agent up by name within a company ...
data "paperclip_agent" "producer" {
  company_id = data.paperclip_company.studio.id
  name       = "Producer"
}

# ... or by identifier.
data "paperclip_agent" "by_id" {
  id = "8b1f6a0c-2d3e-4f5a-9b6c-7d8e9f0a1b2c"
}

# Attach a new agent to an existing manager.
resource "paperclip_agent" "assistant" {
  company_id   = data.paperclip_company.studio.id
  name         = "Production Assistant"
  reports_to   = data.paperclip_agent.producer.id
  adapter_type = "process"
}
