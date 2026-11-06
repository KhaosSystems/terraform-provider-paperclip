// Command terraform-provider-paperclip is the Terraform provider plugin for
// Paperclip. Terraform starts it; it is not meant to be run by hand.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/KhaosSystems/terraform-provider-paperclip/internal/provider"
)

// Generate the registry documentation in docs/ from the schema, templates
// and examples.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name paperclip

// version is set by goreleaser at build time.
var version = "dev"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/KhaosSystems/paperclip",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
