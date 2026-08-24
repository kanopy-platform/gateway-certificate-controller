package challengesolver

import (
	"context"
	"errors"
	"fmt"
	"hash/adler32"
	"time"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerversionedclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	acmev1Client "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1"
	certmanagerinformers "github.com/cert-manager/cert-manager/pkg/client/informers/externalversions"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"

	networkingv1beta1Client "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type ChallengeSolver struct {
	coreClient        corev1listers.ServiceLister
	networkingClient  networkingv1beta1Client.NetworkingV1beta1Interface
	acmeClient        acmev1Client.AcmeV1Interface
	certmanagerClient certmanagerversionedclient.Interface
	glc               *cache.GatewayLookupCache
	dryRun            bool
	plugins           []ChallengePlugin
}

func NewChallengeSolver(cc corev1listers.ServiceLister, nc networkingv1beta1Client.NetworkingV1beta1Interface, cmc certmanagerversionedclient.Interface, glc *cache.GatewayLookupCache, opts ...OptionsFunc) *ChallengeSolver {

	cs := &ChallengeSolver{
		coreClient:        cc,
		networkingClient:  nc,
		glc:               glc,
		certmanagerClient: cmc,
	}

	cs.acmeClient = cs.certmanagerClient.AcmeV1()

	for _, opt := range opts {
		opt(cs)
	}

	return cs
}

func (cs *ChallengeSolver) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	log := log.FromContext(ctx)

	log.Info("Registering controller with Mmanager")

	ctrl, err := controller.New("challengesolver", mgr, controller.Options{
		Reconciler:  cs,
		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](time.Second, 1000*time.Second),
	})

	if err != nil {
		return err
	}

	certmanagerInformerFactory := certmanagerinformers.NewSharedInformerFactoryWithOptions(cs.certmanagerClient, time.Second*30)
	if err := ctrl.Watch(&source.Informer{
		Informer: certmanagerInformerFactory.Acme().V1().Challenges().Informer(),
		Handler:  &handler.EnqueueRequestForObject{},
	}); err != nil {
		return err
	}

	certmanagerInformerFactory.Start(wait.NeverStop)
	certmanagerInformerFactory.WaitForCacheSync(wait.NeverStop)
	return nil

}

func (cs *ChallengeSolver) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {

	log := log.FromContext(ctx)
	log.Info("Reconciling Acme Challenge", "reconcile", req.String())
	log.V(1).Info("Debug")

	challenge, err := cs.acmeClient.Challenges(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			//for a reconciler this likely means deletion, owner references will clean up any existing VirtualServices
			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reconciling challenge, requeued")
		return reconcile.Result{}, err
	}

	err = cs.Solve(ctx, challenge)
	if err != nil {
		//TODO type errors as recoverable or not
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// Solve resolves an ACME HTTP-01 challenge by looking up the gateway and the
// cert-manager solver Service, building a ChallengeMeta, then calling every
// ChallengePlugin whose Applicable method returns true. All applicable plugins
// are attempted regardless of individual errors; any errors are collected and
// returned as a joined error so the reconciler can requeue once.
func (cs *ChallengeSolver) Solve(ctx context.Context, challenge *acmev1.Challenge) error {
	log := log.FromContext(ctx)
	log.V(1).Info("Debug")

	if challenge == nil {
		return nil
	}

	httpDomainHash := cs.Hash(challenge.Spec.DNSName)
	tokenHash := cs.Hash(challenge.Spec.Token)

	namespacedGateway, ok := cs.glc.Get(challenge.Spec.DNSName)
	if !ok {
		// requeue the request to wait for the lookup cache to populate
		return fmt.Errorf("host %s: gateway not found", challenge.Spec.DNSName)
	}
	log.V(1).Info(fmt.Sprintf("Debug: gateway found %s", namespacedGateway))

	svcSet := labels.Set(map[string]string{acmev1.DomainLabelKey: httpDomainHash, acmev1.TokenLabelKey: tokenHash})

	serviceList, err := cs.coreClient.List(svcSet.AsSelector())
	if err != nil {
		return err
	}

	if len(serviceList) == 0 {
		return fmt.Errorf("no service matched selector: %s", fmt.Sprintf("%s=%s,%s=%s", acmev1.DomainLabelKey, httpDomainHash, acmev1.TokenLabelKey, tokenHash))
	}
	svc := serviceList[0]

	if len(svc.Spec.Ports) == 0 {
		return fmt.Errorf("service: %s, missing port definition", svc.Name)
	}

	cm := ChallengeMeta{
		Port:      svc.Spec.Ports[0].Port,
		Service:   svc.Name,
		DNSName:   challenge.Spec.DNSName,
		Namespace: challenge.Namespace,
		Token:     challenge.Spec.Token,
		Name:      challenge.Name,
		UID:       challenge.UID,
		Gateway:   namespacedGateway,
	}

	var errs []error
	for _, p := range cs.plugins {
		if p.Applicable(ctx, challenge) {
			if err := p.Solve(ctx, cm); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (cs *ChallengeSolver) Hash(in string) string {
	return fmt.Sprintf("%d", adler32.Checksum([]byte(in)))
}


