terraform {
  required_providers {
    paperclip = {
      source = "KhaosSystems/paperclip"
    }
  }
}

# The endpoint and API key can also be supplied through the
# PAPERCLIP_API_URL and PAPERCLIP_API_KEY environment variables.
provider "paperclip" {
  endpoint = "http://localhost:3100"

  # Not needed for instances running in the local_trusted deployment mode.
  api_key = var.paperclip_api_key
}

variable "paperclip_api_key" {
  type      = string
  sensitive = true
  default   = null
}
