//go:build tools

// Package tools pins the versions of the build-time tools in go.mod.
package tools

import (
	// Documentation generation.
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
