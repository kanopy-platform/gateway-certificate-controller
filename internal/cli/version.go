package cli

import (
	"fmt"

	"github.com/kanopy-platform/gateway-certificate-controller/internal/version"
	"github.com/spf13/cobra"
)

type versionCommand struct {
	*cobra.Command
}

func newVersionCommand() *cobra.Command {
	cmd := versionCommand{Command: &cobra.Command{}}

	cmd.Use = "version"
	cmd.Short = "Build information for kanopy-gateway-cert-controller"

	cmd.RunE = func(command *cobra.Command, args []string) error {
		fmt.Printf("%#v\n", version.Get())
		return nil
	}

	return cmd.Command
}
