resource "paperclip_agent" "producer" {
  company_id   = paperclip_company.studio.id
  name         = "Producer"
  role         = "ceo"
  title        = "Executive Producer"
  capabilities = "Owns budgets and schedules, and decides which films get made."

  adapter_type = "process"
  adapter_config = jsonencode({
    command    = "python3"
    args       = ["agents/producer.py"]
    cwd        = "/srv/greenlight"
    timeoutSec = 300
  })

  runtime_config = jsonencode({
    heartbeat = {
      enabled     = true
      intervalSec = 300
    }
  })

  budget_monthly_cents = 10000
}

# A whole team from one definition. Each member reports to the producer.
locals {
  crew = {
    director = { title = "Director", capabilities = "Turns scripts into shot lists and reviews dailies." }
    editor   = { title = "Editor", capabilities = "Assembles cuts and prepares deliverables." }
  }
}

resource "paperclip_agent" "crew" {
  for_each = local.crew

  company_id   = paperclip_company.studio.id
  name         = each.value.title
  title        = each.value.title
  capabilities = each.value.capabilities
  reports_to   = paperclip_agent.producer.id

  adapter_type = "http"
  adapter_config = jsonencode({
    url     = "https://agents.example.com/${each.key}/heartbeat"
    method  = "POST"
    headers = { Authorization = "Bearer ${var.agent_webhook_token}" }
  })

  metadata = jsonencode({
    persona = each.key
  })
}

variable "agent_webhook_token" {
  type      = string
  sensitive = true
}
