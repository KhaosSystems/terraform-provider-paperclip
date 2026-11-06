resource "paperclip_project" "pre_production" {
  company_id    = paperclip_company.studio.id
  name          = "Pre-production"
  description   = "Script, storyboard and casting for the first short."
  status        = "in_progress"
  goal_ids      = [paperclip_goal.first_film.id]
  lead_agent_id = paperclip_agent.producer.id
  target_date   = "2027-03-31"
}
