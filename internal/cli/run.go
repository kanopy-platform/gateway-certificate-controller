package cli

import (
	"time"

	certmanagerversionedclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned"
	"github.com/kanopy-platform/cert-gateway-controller/pkg/admission"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"

	"github.com/kanopy-platform/cert-gateway-controller/internal/controller"

	istioinformers "istio.io/client-go/pkg/informers/externalversions"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type runCommand struct {
	k8sFlags *genericclioptions.ConfigFlags
}

func NewRunCommand(k8sFlags *genericclioptions.ConfigFlags) *cobra.Command {
	run := runCommand{k8sFlags}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "run the controller",
		RunE:  run.runE,
	}

	return cmd
}

func (r *runCommand) runE(cmd *cobra.Command, args []string) error {

	cfg, err := r.k8sFlags.ToRESTConfig()
	if err != nil {
		return err
	}

	ic, err := istioversionedclient.NewForConfig(cfg)
	if err != nil {
		return err
	}

	certmanager, err := certmanagerversionedclient.NewForConfig(cfg)

	if err != nil {
		return err
	}

	log.Info("Setting up webhook manager")
	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{})
	if err != nil {
		log.Error(err, "unable to set up overall controller manager")
		return err
	}

	hookServer := mgr.GetWebhookServer()
	hookServer.Port = 8443
	hookServer.CertDir = "/etc/webhook/certs"
	hookServer.Register("/mutate", &webhook.Admission{Handler: admission.GatewayMutationHandler(ic)})

	istioInformerFactory := istioinformers.NewSharedInformerFactory(ic, time.Second*30)

	c := controller.New(
		certmanager,
		ic,
		istioInformerFactory.Networking().V1beta1().Gateways())

	ctx := signals.SetupSignalHandler()
	istioInformerFactory.Start(ctx.Done())

	if err := mgr.Add(c); err != nil {
		return err
	}

	err = mgr.Start(ctx)
	if err != nil {
		return err
	}
	return nil
}
