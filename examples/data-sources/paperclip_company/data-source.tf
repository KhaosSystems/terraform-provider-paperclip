# Look a company up by name ...
data "paperclip_company" "studio" {
  name = "Greenlight Studio"
}

# ... or by identifier.
data "paperclip_company" "by_id" {
  id = "3f0e4a52-6c1b-4f0e-9a43-0d1e6f5a7b21"
}

output "studio_issue_prefix" {
  value = data.paperclip_company.studio.issue_prefix
}
