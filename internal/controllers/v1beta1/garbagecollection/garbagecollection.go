package garbagecollection

import (
	"context"
	"fmt"
	"time"

	certmanagerversionedclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	certmanagerinformers "github.com/cert-manager/cert-manager/pkg/client/informers/externalversions"
	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type GarbageCollectionController struct {
	name              string
	certmanagerClient certmanagerversionedclient.Interface
	istioClient       istioversionedclient.Interface
	dryRun            bool
}

func NewGarbageCollectionController(istioClient istioversionedclient.Interface, certClient certmanagerversionedclient.Interface, opts ...OptionsFunc) *GarbageCollectionController {
	gc := &GarbageCollectionController{
		name:              "istio-garbage-collection-controller",
		certmanagerClient: certClient,
		istioClient:       istioClient,
	}

	for _, opt := range opts {
		opt(gc)
	}

	return gc
}

func (c *GarbageCollectionController) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	ctrl, err := controller.New(c.name, mgr, controller.Options{
		Reconciler:  c,
		RateLimiter: workqueue.NewItemExponentialFailureRateLimiter(time.Second, 1000*time.Second), // maxDelay is limited by the Informer defaultRsync interval
	})
	if err != nil {
		return err
	}

	certmanagerInformerFactory := certmanagerinformers.NewSharedInformerFactoryWithOptions(c.certmanagerClient, time.Second*30, certmanagerinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
		listOptions.LabelSelector = v1beta1labels.ManagedLabelSelector()
	}))

	if err := ctrl.Watch(&source.Informer{Informer: certmanagerInformerFactory.Certmanager().V1().Certificates().Informer()},
		handler.Funcs{
			// only handle Update so that Deleting a certificate does not trigger another Reconcile
			// Create will also trigger an Update
			UpdateFunc: updateFunc,
		}); err != nil {
		return err
	}

	certmanagerInformerFactory.Start(ctx.Done())

	return nil
}

func (c *GarbageCollectionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.V(1).Info("Running Garbage Collection Reconcile", "request", request.String())

	certIface := c.certmanagerClient.CertmanagerV1().Certificates(request.Namespace)
	cert, err := certIface.Get(ctx, request.Name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "failed to Get Certificate")
		return reconcile.Result{
			Requeue: true,
		}, err
	}

	gatewayName, gatewayNamespace := v1beta1labels.ParseManagedLabel(cert.Labels[v1beta1labels.ManagedLabel])

	deleteCert := false
	deleteOptions := metav1.DeleteOptions{}
	if c.dryRun {
		deleteOptions.DryRun = []string{metav1.DryRunAll}
	}

	gateway, err := c.istioClient.NetworkingV1beta1().Gateways(gatewayNamespace).Get(ctx, gatewayName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		log.V(1).Info("Gateway not found, marking Certificate for deletion", "gateway-namespace", gatewayNamespace, "gateway", gatewayName)
		deleteCert = true
	} else if !isCertificateInGatewaySpec(request.Name, gateway) {
		log.V(1).Info("Matching Tls.CredentialName not found, marking Certificate for deletion", "gateway-namespace", gatewayNamespace, "gateway", gatewayName)
		deleteCert = true
	}

	if deleteCert {
		log.Info(fmt.Sprintf("Deleting Certificate %s", request), "dry-run", c.dryRun)
		if err := certIface.Delete(ctx, request.Name, deleteOptions); err != nil {
			log.Error(err, "failed to Delete Certificate")
			return reconcile.Result{
				Requeue: true,
			}, err
		}
	}

	return reconcile.Result{}, nil
}

func updateFunc(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      e.ObjectNew.GetName(),
		Namespace: e.ObjectNew.GetNamespace(),
	}})
}

func isCertificateInGatewaySpec(certificate string, gateway *networkingv1beta1.Gateway) bool {
	for _, s := range gateway.Spec.Servers {
		if s.Tls.CredentialName == certificate {
			return true
		}
	}
	return false
}
