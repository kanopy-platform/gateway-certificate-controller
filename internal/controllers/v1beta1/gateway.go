package v1beta1

import (
	"context"
	"time"

	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"

	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformers "istio.io/client-go/pkg/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type GatewayController struct {
	istioClient istioversionedclient.Interface
	name        string
}

func NewGatewayController(client istioversionedclient.Interface) *GatewayController {
	gr := &GatewayController{name: "istio-gateway-controller", istioClient: client}
	return gr
}

func (c *GatewayController) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	ctrl, err := controller.New("istio-gateway-controller", mgr, controller.Options{
		Reconciler: c,
	})
	if err != nil {
		return err
	}

	istioInformerFactory := istioinformers.NewSharedInformerFactoryWithOptions(c.istioClient, time.Second*30, istioinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
		listOptions.LabelSelector = v1beta1labels.InjectSimpleCredentialNameLabelSelector()
	}))

	if err := ctrl.Watch(&source.Informer{Informer: istioInformerFactory.Networking().V1beta1().Gateways().Informer()},
		&handler.EnqueueRequestForObject{}); err != nil {
		return err
	}

	istioInformerFactory.Start(ctx.Done())

	return nil
}

func (r *GatewayController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// set up a convenient log object so we don't have to type request over and over again
	log := log.FromContext(ctx)

	log.Info("Reconciling Gateway...")

	return reconcile.Result{}, nil
}
