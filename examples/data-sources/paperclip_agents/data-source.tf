data "paperclip_agents" "studio" {
  company_id = data.paperclip_company.studio.id
}

output "agent_names" {
  value = [for agent in data.paperclip_agents.studio.agents : agent.name]
}

# Identifiers of everyone who reports to nobody, i.e. the top of the organisation.
output "leadership" {
  value = [for agent in data.paperclip_agents.studio.agents : agent.id if agent.reports_to == null]
}
