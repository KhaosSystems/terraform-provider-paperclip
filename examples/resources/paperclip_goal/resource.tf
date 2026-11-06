resource "paperclip_goal" "mission" {
  company_id = paperclip_company.studio.id
  title      = "Become the studio festivals call first"
  level      = "company"
  status     = "active"
}

resource "paperclip_goal" "first_film" {
  company_id     = paperclip_company.studio.id
  title          = "Release a ten minute short this quarter"
  description    = "Festival ready, with subtitles in three languages."
  level          = "team"
  parent_id      = paperclip_goal.mission.id
  owner_agent_id = paperclip_agent.producer.id
}
