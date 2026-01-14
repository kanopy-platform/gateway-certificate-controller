package orderwatcher

import (
	"context"
	"fmt"
	"time"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerversionedclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	certmanagerinformers "github.com/cert-manager/cert-manager/pkg/client/informers/externalversions"

	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"

	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// OrderWatcher watches cert-manager Order resources and emits Kubernetes events
// on the associated Gateway when certificates are successfully issued or fail.
type OrderWatcher struct {
	certmanagerClient certmanagerversionedclient.Interface
	istioClient       istioversionedclient.Interface
	recorder          record.EventRecorder
	dryRun            bool
	eventConfig       EventConfig

	// processedOrders tracks which orders we've already emitted events for
	// to avoid duplicate events on requeue.
	processedOrders map[string]acmev1.State
}

func NewOrderWatcher(cmc certmanagerversionedclient.Interface, ic istioversionedclient.Interface, recorder record.EventRecorder, opts ...OptionsFunc) *OrderWatcher {
	ow := &OrderWatcher{
		certmanagerClient: cmc,
		istioClient:       ic,
		recorder:          recorder,
		processedOrders:   make(map[string]acmev1.State),
		eventConfig: EventConfig{
			CertificateReadyReason:  "CertificateReady",
			CertificateFailedReason: "ChallengeFailed",
		},
	}

	for _, opt := range opts {
		opt(ow)
	}

	return ow
}

func (ow *OrderWatcher) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	log := log.FromContext(ctx)
	log.Info("Registering OrderWatcher controller with Manager")

	ctrl, err := controller.New("orderwatcher", mgr, controller.Options{
		Reconciler:  ow,
		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](time.Second, 1000*time.Second),
	})
	if err != nil {
		return err
	}

	certmanagerInformerFactory := certmanagerinformers.NewSharedInformerFactoryWithOptions(ow.certmanagerClient, time.Second*30)
	if err := ctrl.Watch(&source.Informer{
		Informer: certmanagerInformerFactory.Acme().V1().Orders().Informer(),
		Handler:  &handler.EnqueueRequestForObject{},
	}); err != nil {
		return err
	}

	certmanagerInformerFactory.Start(wait.NeverStop)
	certmanagerInformerFactory.WaitForCacheSync(wait.NeverStop)

	return nil
}

func (ow *OrderWatcher) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	log.V(1).Info("Reconciling Order", "reconcile", req.String())

	order, err := ow.certmanagerClient.AcmeV1().Orders(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			delete(ow.processedOrders, req.String())
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, err
	}

	if order.Status.State == "" || order.Status.State == acmev1.Pending {
		return reconcile.Result{}, nil
	}

	orderKey := req.String()
	if prevState, seen := ow.processedOrders[orderKey]; seen && prevState == order.Status.State {
		return reconcile.Result{}, nil
	}

	gateway, err := ow.findGatewayForOrder(ctx, order)
	if err != nil {
		log.V(1).Info("Could not find gateway for order", "order", req.String(), "error", err)
		return reconcile.Result{}, nil
	}

	if gateway == nil {
		return reconcile.Result{}, nil
	}

	ow.processedOrders[orderKey] = order.Status.State

	switch order.Status.State {
	case acmev1.Valid:
		ow.emitEvent(gateway, corev1.EventTypeNormal, ow.eventConfig.CertificateReadyReason,
			fmt.Sprintf("Certificate for %s has been successfully issued", getDNSNamesFromOrder(order)))
	case acmev1.Invalid, acmev1.Errored:
		ow.emitEvent(gateway, corev1.EventTypeWarning, ow.eventConfig.CertificateFailedReason,
			fmt.Sprintf("Certificate challenge failed for %s: %s", getDNSNamesFromOrder(order), order.Status.Reason))
	}

	return reconcile.Result{}, nil
}

// findGatewayForOrder traverses the owner reference chain from Order -> CertificateRequest -> Certificate
// to find the managed label that points back to the Gateway.
func (ow *OrderWatcher) findGatewayForOrder(ctx context.Context, order *acmev1.Order) (interface{}, error) {
	certReqRef := findOwnerRef(order.OwnerReferences, "CertificateRequest")
	if certReqRef == nil {
		return nil, fmt.Errorf("order has no CertificateRequest owner")
	}

	certReq, err := ow.certmanagerClient.CertmanagerV1().CertificateRequests(order.Namespace).Get(ctx, certReqRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	certRef := findOwnerRef(certReq.OwnerReferences, "Certificate")
	if certRef == nil {
		return nil, fmt.Errorf("certificate request has no Certificate owner")
	}

	cert, err := ow.certmanagerClient.CertmanagerV1().Certificates(order.Namespace).Get(ctx, certRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	managedLabel, ok := cert.Labels[v1beta1labels.ManagedLabel]
	if !ok {
		return nil, fmt.Errorf("certificate is not managed by this controller")
	}

	gatewayName, namespace := v1beta1labels.ParseManagedLabel(managedLabel)
	if gatewayName == "" || namespace == "" {
		return nil, fmt.Errorf("invalid managed label format: %s", managedLabel)
	}

	gateway, err := ow.istioClient.NetworkingV1beta1().Gateways(namespace).Get(ctx, gatewayName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return gateway, nil
}

func (ow *OrderWatcher) emitEvent(obj interface{}, eventType, reason, message string) {
	if ow.dryRun {
		return
	}

	if runtimeObj, ok := obj.(runtime.Object); ok {
		ow.recorder.Event(runtimeObj, eventType, reason, message)
	}
}

func findOwnerRef(refs []metav1.OwnerReference, kind string) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Kind == kind {
			return &refs[i]
		}
	}
	return nil
}

func getDNSNamesFromOrder(order *acmev1.Order) string {
	if len(order.Spec.DNSNames) == 0 {
		return "unknown"
	}
	return order.Spec.DNSNames[0]
}
