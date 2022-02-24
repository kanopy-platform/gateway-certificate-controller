package cli

import (
	"github.com/spf13/pflag"
)

func flagLogLevel(flags *pflag.FlagSet) {
	flags.String("log-level", "info", "Configure log level")
}
