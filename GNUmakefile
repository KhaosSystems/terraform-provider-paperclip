default: build

BINARY := terraform-provider-paperclip

# Acceptance tests run against an in-memory fake of the Paperclip API unless
# PAPERCLIP_API_URL points at a real instance.
ACC_TIMEOUT ?= 30m

.PHONY: build install fmt lint test testacc testacc-tofu generate docs-check tidy

build:
	go build -o $(BINARY) .

install:
	go install .

fmt:
	gofmt -s -w .
	terraform fmt -recursive examples || tofu fmt -recursive examples

lint:
	golangci-lint run

test:
	go test ./... -race -cover -timeout 5m

testacc:
	TF_ACC=1 go test ./internal/provider/ -run '^TestAcc' -count=1 -timeout $(ACC_TIMEOUT) -v

# Same as testacc, but drives OpenTofu instead of Terraform.
testacc-tofu:
	TF_ACC=1 \
	TF_ACC_TERRAFORM_PATH="$$(command -v tofu)" \
	TF_ACC_PROVIDER_NAMESPACE=hashicorp \
	TF_ACC_PROVIDER_HOST=registry.opentofu.org \
	go test ./internal/provider/ -run '^TestAcc' -count=1 -timeout $(ACC_TIMEOUT) -v

generate:
	go generate ./...

# Fails when the generated documentation is out of date.
docs-check: generate
	git diff --exit-code -- docs

tidy:
	go mod tidy
