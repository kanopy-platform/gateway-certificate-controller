package challengesolver

import (
	"context"
	"fmt"
	"hash/adler32"
	"time"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerversionedclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	acmev1Client "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1"
	certmanagerinformers "github.com/cert-manager/cert-manager/pkg/client/informers/externalversions"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"

	apinetv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	netapplymetav1 "istio.io/client-go/pkg/applyconfiguration/meta/v1"
	netapplyv1beta1 "istio.io/client-go/pkg/applyconfiguration/networking/v1beta1"
	networkingv1beta1Client "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1"

	istiov1beta1 "istio.io/api/networking/v1beta1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
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
		RateLimiter: workqueue.NewItemExponentialFailureRateLimiter(time.Second, 1000*time.Second),
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
		if errors.IsNotFound(err) {
			//for a reconciler this likely means deletion, owner references will clean up any existing VirtualServices
			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reconciling challenge, requeued")
		return reconcile.Result{
			Requeue: true,
		}, err
	}

	_, err = cs.Solve(ctx, challenge)
	if err != nil {
		//TODO type errors as recoverable or not
		return reconcile.Result{
			Requeue: true,
		}, err
	}

	return reconcile.Result{}, nil
}

func (cs *ChallengeSolver) Solve(ctx context.Context, challenge *acmev1.Challenge) (*apinetv1beta1.VirtualService, error) {
	log := log.FromContext(ctx)
	log.V(1).Info("Debug")

	if challenge == nil {
		return nil, nil
	}

	httpDomainHash := cs.Hash(challenge.Spec.DNSName)
	tokenHash := cs.Hash(challenge.Spec.Token)

	namespacedGateway, ok := cs.glc.Get(challenge.Spec.DNSName)
	if !ok {
		// requeue the request to wait for the lookup cache to populate
		// probably needs backoff
		return nil, fmt.Errorf("host %s: gateway not found", challenge.Spec.DNSName)
	}
	log.V(1).Info(fmt.Sprintf("Debug: gateway found %s", namespacedGateway))

	svcSet := labels.Set(map[string]string{acmev1.DomainLabelKey: httpDomainHash, acmev1.TokenLabelKey: tokenHash})

	serviceList, err := cs.coreClient.List(svcSet.AsSelector())
	if err != nil {
		// requeue the request to wait for the service to appear in the api
		return nil, err
	}

	if len(serviceList) == 0 {
		// requeue the request to wait for the service to appear in the api
		return nil, fmt.Errorf("no service matched selector: %s", fmt.Sprintf("%s=%s,%s=%s", acmev1.DomainLabelKey, httpDomainHash, acmev1.TokenLabelKey, tokenHash))
	}
	svc := serviceList[0]

	if len(svc.Spec.Ports) == 0 {
		// this is probably unrecoverable
		return nil, fmt.Errorf("service: %s, missing port definition", svc.Name)
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
	vsApply := VirtualServiceApplyFromChallengeMeta(cm)

	// This controller is authoritative for these virtualservices so stomp any old versions that exist
	if cs.dryRun {
		log.Info(fmt.Sprintf("dry-run: patching %s.%s %s/%s", *vsApply.Kind, *vsApply.APIVersion, *vsApply.Namespace, *vsApply.Name))
		return nil, nil
	}

	return cs.networkingClient.VirtualServices(challenge.Namespace).Apply(ctx, vsApply, metav1.ApplyOptions{Force: true, FieldManager: "challengesolver"})
}

func (cs *ChallengeSolver) Hash(in string) string {
	return fmt.Sprintf("%d", adler32.Checksum([]byte(in)))
}

type ChallengeMeta struct {
	Port      int32
	Service   string
	DNSName   string
	Namespace string
	Token     string
	Name      string
	UID       types.UID
	Gateway   string
}

func VirtualServiceApplyFromChallengeMeta(cm ChallengeMeta) *netapplyv1beta1.VirtualServiceApplyConfiguration {

	vsAPIVersion := apinetv1beta1.SchemeGroupVersion.String()
	vsKind := "VirtualService"

	vsApply := netapplyv1beta1.VirtualServiceApplyConfiguration{
		ObjectMetaApplyConfiguration: &netapplymetav1.ObjectMetaApplyConfiguration{},
		Spec: &istiov1beta1.VirtualService{
			Hosts:    []string{cm.DNSName},
			Gateways: []string{cm.Gateway},
			Http: []*istiov1beta1.HTTPRoute{
				{
					Name: "solver",
					Match: []*istiov1beta1.HTTPMatchRequest{
						{
							Uri: &istiov1beta1.StringMatch{
								MatchType: &istiov1beta1.StringMatch_Exact{
									Exact: fmt.Sprintf("/.well-known/acme-challenge/%s", cm.Token),
								},
							},
						},
					},
					Route: []*istiov1beta1.HTTPRouteDestination{
						{
							Destination: &istiov1beta1.Destination{
								Host: cm.Service,
								Port: &istiov1beta1.PortSelector{
									Number: uint32(cm.Port),
								},
							},
						},
					},
				},
			},
		},
	}

	vsApply.APIVersion = &vsAPIVersion
	vsApply.Kind = &vsKind

	apiVersion := acmev1.SchemeGroupVersion.String()
	kind := "Challenge"
	vsApply.Namespace = &cm.Namespace
	vsApply.Name = &cm.Name
	vsApply.OwnerReferences = append(vsApply.OwnerReferences, netapplymetav1.OwnerReferenceApplyConfiguration{
		APIVersion: &apiVersion,
		Kind:       &kind,
		Name:       &cm.Name,
		UID:        &cm.UID,
	})

	return &vsApply
}
