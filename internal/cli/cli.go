package cli

import (
	"strings"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	v1beta1controllers "github.com/kanopy-platform/gateway-certificate-controller/internal/controllers/v1beta1"
	logzap "github.com/kanopy-platform/gateway-certificate-controller/internal/log/zap"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	"istio.io/pkg/log"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	klog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(networkingv1beta1.SchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.SchemeBuilder.AddToScheme(scheme))

}

type RootCommand struct {
	k8sFlags *genericclioptions.ConfigFlags
}

func NewRootCommand() *cobra.Command {
	k8sFlags := genericclioptions.NewConfigFlags(true)

	root := &RootCommand{k8sFlags}

	cmd := &cobra.Command{
		Use:               "kanopy-gateway-cert-controller",
		PersistentPreRunE: root.persistentPreRunE,
		RunE:              root.runE,
	}

	cmd.PersistentFlags().String("log-level", "info", "Configure log level")
	cmd.PersistentFlags().Int("webhook-listen-port", 8443, "Admission webhook listen port")
	cmd.PersistentFlags().String("webhook-certs-dir", "/etc/webhook/certs", "Admission webhook TLS certificate directory")
	cmd.PersistentFlags().Bool("dry-run", false, "Controller dry-run changes only")

	k8sFlags.AddFlags(cmd.PersistentFlags())
	// no need to check err, this only checks if variadic args != 0
	_ = viper.BindEnv("kubeconfig", "KUBECONFIG")

	cmd.AddCommand(newVersionCommand())
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
	logLevel, err := logzap.ParseLevel(viper.GetString("log-level"))
	if err != nil {
		return err
	}

	klog.SetLogger(zap.New(zap.Level(logLevel)))

	return nil
}

func (c *RootCommand) runE(cmd *cobra.Command, args []string) error {
	dryRun := viper.GetBool("dry-run")
	if dryRun {
		klog.Log.Info("running in dry-run mode.")
	}

	cfg, err := c.k8sFlags.ToRESTConfig()
	if err != nil {
		return err
	}

	ic, err := istioversionedclient.NewForConfig(cfg)
	if err != nil {
		return err
	}

	ctx := signals.SetupSignalHandler()

	mgr, err := manager.New(cfg, manager.Options{
		Scheme:                 scheme,
		Host:                   "0.0.0.0",
		Port:                   viper.GetInt("webhook-listen-port"),
		CertDir:                viper.GetString("webhook-certs-dir"),
		MetricsBindAddress:     "0.0.0.0:80",
		HealthProbeBindAddress: ":8080",
		LeaderElection:         true,
		LeaderElectionID:       "kanopy-gateway-cert-controller",
		DryRunClient:           viper.GetBool("dry-run"),
	})

	if err != nil {
		log.Error(err, "unable to set up  controller manager")
		return err
	}

	if err := configureHealthChecks(mgr); err != nil {
		return err
	}

	if err := v1beta1controllers.NewGatewayController(ic).SetupWithManager(ctx, mgr); err != nil {
		return err
	}

	// TODO add mutate handler

	return mgr.Start(ctx)
}

func configureHealthChecks(mgr manager.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	return nil
}
