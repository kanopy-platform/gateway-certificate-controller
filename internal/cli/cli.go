package cli

import (
	"strings"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type RootCommand struct{}

func NewRootCommand() *cobra.Command {
	root := &RootCommand{}

	cmd := &cobra.Command{
		Use:               "cert-gateway-controller",
		PersistentPreRunE: root.persistentPreRunE,
	}

	flags := cmd.PersistentFlags()
	flagLogLevel(flags)

	k8sFlags := genericclioptions.NewConfigFlags(true)
	k8sFlags.AddFlags(flags)
	// no need to check err, this only checks if variadic args != 0
	_ = viper.BindEnv("kubeconfig", "KUBECONFIG")

	// subdommands
	cmd.AddCommand(NewRunCommand(k8sFlags))

	return cmd
}

func (c *RootCommand) persistentPreRunE(cmd *cobra.Command, args []string) error {
	// bind flags to viper
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.SetEnvPrefix("app")
	viper.AutomaticEnv()

	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		return err
	}

	// set log level
	logLevel, err := log.ParseLevel(viper.GetString("log-level"))
	if err != nil {
		return err
	}

	log.SetLevel(logLevel)

	return nil
}
