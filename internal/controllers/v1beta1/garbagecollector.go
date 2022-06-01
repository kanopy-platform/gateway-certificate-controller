package v1beta1

import (
	"context"
	"fmt"
	"time"

	certmanagerversionedclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	certmanagerinformers "github.com/cert-manager/cert-manager/pkg/client/informers/externalversions"
	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"
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
}

func NewGarbageCollectionController(certmanagerClient certmanagerversionedclient.Interface, istioClient istioversionedclient.Interface) *GarbageCollectionController {
	gc := &GarbageCollectionController{
		name:              "istio-garbage-collection-controller",
		certmanagerClient: certmanagerClient,
		istioClient:       istioClient,
	}
	return gc
}

func (c *GarbageCollectionController) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	ctrl, err := controller.New(c.name, mgr, controller.Options{
		Reconciler: c,
	})
	if err != nil {
		return err
	}

	certmanagerInformerFactory := certmanagerinformers.NewSharedInformerFactoryWithOptions(c.certmanagerClient, time.Second*10, certmanagerinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
		listOptions.LabelSelector = v1beta1labels.ManagedLabelSelector()
	}))

	if err := ctrl.Watch(&source.Informer{Informer: certmanagerInformerFactory.Certmanager().V1().Certificates().Informer()},
		handler.Funcs{
			// only queue on Update so that Delete does not trigger another Reconcile
			// Add will trigger an Update
			UpdateFunc: updateFunc,
		}); err != nil {

		//&handler.EnqueueRequestForObject{}); err != nil {
		return err
	}

	certmanagerInformerFactory.Start(ctx.Done())

	return nil
}

func (c *GarbageCollectionController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// set up a convenient log object so we don't have to type request over and over again
	log := log.FromContext(ctx)

	log.Info("Reconciling Garbage Collection...")

	certIface := c.certmanagerClient.CertmanagerV1().Certificates(request.Namespace)
	cert, err := certIface.Get(ctx, request.Name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "failed to Get Certificate")
		return reconcile.Result{}, err
	}

	label := cert.Labels[v1beta1labels.ManagedLabelString()]
	gatewayName, gatewayNamespace := v1beta1labels.ManagedLabelValues(label)

	_, err = c.istioClient.NetworkingV1beta1().Gateways(gatewayNamespace).Get(ctx, gatewayName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		log.Info(fmt.Sprintf("Gateway not found, deleting Certificate %s", request))
		if err := certIface.Delete(ctx, request.Name, metav1.DeleteOptions{}); err != nil { // TODO add dryRun to DeleteOptions
			log.Error(err, "failed to Delete Certificate")
			return reconcile.Result{}, err
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
