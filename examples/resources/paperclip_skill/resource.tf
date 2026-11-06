resource "paperclip_skill" "greenlight" {
  company_id = paperclip_company.studio.id
  name       = "Greenlight API"
  markdown   = file("${path.module}/skills/greenlight/SKILL.md")
}

# Assign it to an agent through its key.
resource "paperclip_agent" "director" {
  company_id   = paperclip_company.studio.id
  name         = "Director"
  adapter_type = "codex_local"
  skills       = [paperclip_skill.greenlight.key]
}
