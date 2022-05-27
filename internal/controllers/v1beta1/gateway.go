package v1beta1

import (
	"context"
	"time"

	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"

	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformers "istio.io/client-go/pkg/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type eventReconciler interface {
	Cleanup(ctx context.Context, request reconcile.Request) error
}

type eventHandler struct{}

type GatewayController struct {
	istioClient istioversionedclient.Interface
	name        string
	events      eventReconciler
}

func NewGatewayController(client istioversionedclient.Interface) *GatewayController {
	gr := &GatewayController{name: "istio-gateway-controller", istioClient: client, events: &eventHandler{}}
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

func (c *GatewayController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// set up a convenient log object so we don't have to type request over and over again
	log := log.FromContext(ctx)
	log.Info("Reconciling Gateway...", "reconcile ", request.String())
	gateway, err := c.istioClient.NetworkingV1beta1().Gateways(request.Namespace).Get(ctx, request.Name, metav1.GetOptions{})

	if err != nil {
		if errors.IsNotFound(err) {
			// check and delete cert
			log.Info("Cleaned up certificates")
			if err := c.events.Cleanup(ctx, request); err != nil {
				return reconcile.Result{Requeue: true}, err
			}
			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reconciling gateway, requeued")
		return reconcile.Result{
			Requeue: true,
		}, err
	}

	for _, s := range gateway.Spec.Servers {
		log.V(4).Info("Inspecting", "server", s.Hosts)
	}

	return reconcile.Result{}, nil
}

func (e *eventHandler) Cleanup(ctx context.Context, request reconcile.Request) error {
	return nil
}
