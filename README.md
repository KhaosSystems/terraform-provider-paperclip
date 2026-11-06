# Terraform Provider for Paperclip

Manage a [Paperclip](https://paperclip.ing) agent organisation as code: companies, agents, goals and projects are declared in Terraform, reviewed in pull requests and applied like any other infrastructure.

> This is a community provider. It is not affiliated with or endorsed by the Paperclip project.

## Status

Early development. The schema may still change before `v1.0.0`.

The provider is tested against an in-memory fake of the Paperclip API that was written from the upstream route handlers and validators. It has not yet been run against a live Paperclip instance; see [Testing against a real instance](#testing-against-a-real-instance) before relying on it.

## Usage

```terraform
terraform {
  required_providers {
    paperclip = {
      source = "KhaosSystems/paperclip"
    }
  }
}

provider "paperclip" {
  endpoint = "http://localhost:3100" # or PAPERCLIP_API_URL
  # api_key = var.paperclip_api_key  # or PAPERCLIP_API_KEY
}

resource "paperclip_company" "studio" {
  name                                  = "Greenlight Studio"
  require_board_approval_for_new_agents = false
}

resource "paperclip_agent" "producer" {
  company_id   = paperclip_company.studio.id
  name         = "Producer"
  role         = "ceo"
  adapter_type = "process"
  adapter_config = jsonencode({
    command = "python3"
    args    = ["agents/producer.py"]
  })
  runtime_config = jsonencode({
    heartbeat = { enabled = true, intervalSec = 300 }
  })
  can_create_agents = false
}

resource "paperclip_skill" "greenlight" {
  company_id = paperclip_company.studio.id
  name       = "Greenlight API"
  markdown   = file("${path.module}/skills/greenlight/SKILL.md")
}

resource "paperclip_agent" "director" {
  company_id        = paperclip_company.studio.id
  name              = "Director"
  reports_to        = paperclip_agent.producer.id
  adapter_type      = "codex_local"
  instructions      = file("${path.module}/agents/director/AGENTS.md")
  skills            = [paperclip_skill.greenlight.key]
  can_create_agents = false
}
```

Full reference documentation is in [`docs/`](docs/index.md), with runnable snippets in [`examples/`](examples).

| Type        | Name                | Purpose                                         |
| ----------- | ------------------- | ----------------------------------------------- |
| Resource    | `paperclip_company` | A company, the container for everything else    |
| Resource    | `paperclip_agent`   | An agent, with its adapter and runtime settings |
| Resource    | `paperclip_goal`    | A goal in the company's goal tree               |
| Resource    | `paperclip_project` | A project linked to goals and a lead agent      |
| Resource    | `paperclip_skill`   | A skill in the company's library                |
| Data source | `paperclip_company` | Look a company up by id or name                 |
| Data source | `paperclip_agent`   | Look an agent up by id, or by name in a company |
| Data source | `paperclip_agents`  | List the agents of a company                    |

### Authentication

| Deployment mode | What to configure                                                 |
| --------------- | ----------------------------------------------------------------- |
| `local_trusted` | Only `endpoint`. The instance trusts every local caller.          |
| `authenticated` | `endpoint` and `api_key`, a board API key sent as a bearer token. |

Creating companies needs instance-admin rights.

### Things to know

- **Agents are created directly.** `paperclip_agent` does not go through the hire-approval flow, which Paperclip only allows when the company has `require_board_approval_for_new_agents = false`.
- **Only declared settings are tracked.** For `adapter_config` and `runtime_config`, defaults that Paperclip adds on its own are not reported as drift, and keys added outside Terraform go unnoticed.
- **Secrets are not read back.** Paperclip redacts plain values under `adapter_config.env`. The provider assumes they are unchanged and never writes the redaction placeholder back. Plain values are still stored in the Terraform state, so prefer secret references.
- **Destroying a company deletes everything in it,** including tasks and history that Terraform never managed.
- **Runtime data is out of scope.** Tasks, approvals and runs change constantly while agents work and do not belong in Terraform state.
- **Instructions need a supporting adapter.** `instructions` manages the entry file only, and `process` and `http` agents have no instruction bundle. Agents can edit their own instructions; the next apply puts the declared content back.
- **`skills` is authoritative when set.** It replaces whatever else is assigned. Leave it out to manage assignments elsewhere.
- **Agents can hire by default.** Set `can_create_agents = false` to keep the organisation as declared.
- **Task assignment is not an attribute.** The API reports every active agent as able to assign tasks, so the provider could not confirm a declared value. It keeps the existing grant unchanged.
- **Destroying a skill detaches it first,** from every agent using it, including agents Terraform does not manage. A warning names them.

## Development

Requirements: [Go](https://go.dev/dl/) 1.25 or newer and either [Terraform](https://developer.hashicorp.com/terraform/install) 1.5+ or [OpenTofu](https://opentofu.org/docs/intro/install/).

| Command             | What it does                                                      |
| ------------------- | ----------------------------------------------------------------- |
| `make build`        | Build the provider binary                                         |
| `make test`         | Unit tests with the race detector                                 |
| `make testacc`      | Acceptance tests, driving Terraform                               |
| `make testacc-tofu` | Acceptance tests, driving OpenTofu                                |
| `make lint`         | `golangci-lint`                                                   |
| `make generate`     | Regenerate `docs/` from the schema and `examples/`                |

### How the tests are organised

- `internal/client` has unit tests for the HTTP client: request shapes, error decoding and retry rules.
- `internal/provider` has unit tests for the JSON drift and redaction logic, a schema self-check, and acceptance tests that drive a real Terraform binary through create, update, import and destroy.

Acceptance tests run against the fake in [`fake_server_test.go`](internal/provider/fake_server_test.go) unless `PAPERCLIP_API_URL` is set, so they need no instance and no secrets.

### Testing against a real instance

Point the same suite at a real instance. It creates and deletes companies whose names start with `tf-acc-`, so use a disposable instance.

```shell
export PAPERCLIP_API_URL=http://localhost:3100
export PAPERCLIP_API_KEY=...   # only for authenticated instances
make testacc
```

The `Acceptance (live Paperclip)` workflow does the same on GitHub Actions against the official container image. It runs weekly and on demand, and does not gate pull requests.

### Using a local build

Add a development override to your [CLI configuration file](https://developer.hashicorp.com/terraform/cli/config/config-file) so Terraform uses the binary from `go install`:

```hcl
provider_installation {
  dev_overrides {
    "KhaosSystems/paperclip" = "/path/to/your/go/bin"
  }
  direct {}
}
```

### Releasing

Push a `v*` tag. The release workflow builds, signs and publishes the artefacts in the layout the Terraform Registry expects. It needs the `GPG_PRIVATE_KEY` and `PASSPHRASE` repository secrets.

## License

[MIT](LICENSE)
