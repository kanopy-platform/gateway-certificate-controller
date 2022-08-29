package cli

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	// import oidc auth
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/cache"

	"github.com/kanopy-platform/gateway-certificate-controller/internal/admission"
	v1beta1gc "github.com/kanopy-platform/gateway-certificate-controller/internal/controllers/v1beta1/garbagecollection"
	v1beta1controllers "github.com/kanopy-platform/gateway-certificate-controller/internal/controllers/v1beta1/gateway"
	logzap "github.com/kanopy-platform/gateway-certificate-controller/internal/log/zap"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagerversionedclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sinformers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
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

//RootCommand is the origin of all command life
type RootCommand struct {
	k8sFlags *genericclioptions.ConfigFlags
}

//NewRootCommand seeds the new life of a root command
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
	cmd.PersistentFlags().Int("metrics-listen-port", 8081, "Admission webhook listen port")
	cmd.PersistentFlags().String("webhook-certs-dir", "/etc/webhook/certs", "Admission webhook TLS certificate directory")
	cmd.PersistentFlags().Bool("external-dns", false, "Enable external-dns support")
	cmd.PersistentFlags().String("external-dns-target", "", "Target to use for the external-dns target annotation value, implies --external-dns, default disabled")
	cmd.PersistentFlags().String("external-dns-selector", "", "Valid label selector string to use for excluding namespace from butation, implies --external-dns")
	cmd.PersistentFlags().Bool("dry-run", false, "Controller dry-run changes only")
	cmd.PersistentFlags().String("certificate-namespace", "cert-manager", "Namespace that stores Certificates")
	cmd.PersistentFlags().String("default-issuer", "selfsigned", "The default ClusterIssuer")

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
		klog.Log.Info("running in dry-run mode")
	}

	cfg, err := c.k8sFlags.ToRESTConfig()
	if err != nil {
		return err
	}

	ic, err := istioversionedclient.NewForConfig(cfg)
	if err != nil {
		return err
	}

	cmc, err := certmanagerversionedclient.NewForConfig(cfg)
	if err != nil {
		return err
	}

	ctx := signals.SetupSignalHandler()

	mgr, err := manager.New(cfg, manager.Options{
		Scheme:                 scheme,
		Host:                   "0.0.0.0",
		Port:                   viper.GetInt("webhook-listen-port"),
		CertDir:                viper.GetString("webhook-certs-dir"),
		MetricsBindAddress:     fmt.Sprintf("0.0.0.0:%d", viper.GetInt("metrics-listen-port")),
		HealthProbeBindAddress: ":8080",
		LeaderElection:         true,
		LeaderElectionID:       "kanopy-gateway-cert-controller",
		DryRunClient:           dryRun,
	})

	if err != nil {
		klog.Log.Error(err, "unable to set up  controller manager")
		return err
	}

	if err := configureHealthChecks(mgr); err != nil {
		return err
	}

	if err := v1beta1controllers.NewGatewayController(ic, cmc,
		v1beta1controllers.WithDryRun(viper.GetBool("dry-run")),
		v1beta1controllers.WithDefaultClusterIssuer(viper.GetString("default-issuer")),
		v1beta1controllers.WithCertificateNamespace(viper.GetString("certificate-namespace"))).
		SetupWithManager(ctx, mgr); err != nil {
		return err
	}

	if err := v1beta1gc.NewGarbageCollectionController(ic, cmc,
		v1beta1gc.WithDryRun(dryRun)).
		SetupWithManager(ctx, mgr); err != nil {
		return err
	}

	edc := admission.NewExternalDNSConfig()
	externalDNSTarget := viper.GetString("external-dns-target")
	externalDNSSelector := viper.GetString("external-dns-selector")

	//externalDNS settings are enabled with defaults via --external-dns or implictly by overriding defaults
	//with either flag
	externalDNSEnabled := viper.GetBool("external-dns")
	if externalDNSTarget != "" || externalDNSSelector != "" {
		externalDNSEnabled = true
	}

	if externalDNSTarget != "" {
		edc.SetTarget(externalDNSTarget)
	}

	if externalDNSSelector != "" {
		err := edc.SetSelector(externalDNSSelector)
		if err != nil {
			return err
		}
	}

	edc.SetEnabled(externalDNSEnabled)

	var clientset *kubernetes.Clientset
	var k8sInformerFactory k8sinformers.SharedInformerFactory
	var nsInformer corev1informers.NamespaceInformer
	var nsl corev1listers.NamespaceLister
	if externalDNSEnabled {
		clientset, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			return err
		}

		k8sInformerFactory = k8sinformers.NewSharedInformerFactoryWithOptions(clientset, time.Second*30)
		nsInformer = k8sInformerFactory.Core().V1().Namespaces()

		//need at least one listener func to populate the in memory cache
		nsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(new interface{}) {},
		})

		k8sInformerFactory.Start(wait.NeverStop)
		k8sInformerFactory.WaitForCacheSync(wait.NeverStop)
		nsl = nsInformer.Lister()
	}

	admission.NewGatewayMutationHook(
		ic,
		nsl,
		admission.WithExternalDNSConfig(edc)).SetupWithManager(mgr)

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
