package challengesolver

import (
	"context"
	"fmt"
	"hash/adler32"
	"strings"
	"time"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerversionedclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	acmev1Client "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1"
	certmanagerinformers "github.com/cert-manager/cert-manager/pkg/client/informers/externalversions"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"

	apinetv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	netapplymetav1 "istio.io/client-go/pkg/applyconfiguration/meta/v1"
	netapplyv1beta1 "istio.io/client-go/pkg/applyconfiguration/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	networkingv1beta1Client "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1"

	istiov1beta1 "istio.io/api/networking/v1beta1"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
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
	istioClient       istioversionedclient.Interface
	k8sClient         kubernetes.Interface
	glc               *cache.GatewayLookupCache
	dryRun            bool
	fallbackIngress   FallbackIngressConfig
}

func NewChallengeSolver(cc corev1listers.ServiceLister, ic istioversionedclient.Interface, cmc certmanagerversionedclient.Interface, k8s kubernetes.Interface, glc *cache.GatewayLookupCache, opts ...OptionsFunc) *ChallengeSolver {

	cs := &ChallengeSolver{
		coreClient:        cc,
		networkingClient:  ic.NetworkingV1beta1(),
		istioClient:       ic,
		glc:               glc,
		certmanagerClient: cmc,
		k8sClient:         k8s,
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

// Solve creates the appropriate routing resource (Ingress or VirtualService) to route
// ACME HTTP-01 challenge traffic to the cert-manager solver service.
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

	useFallbackIngress, err := cs.shouldUseFallbackIngress(ctx, namespacedGateway)
	if err != nil {
		return nil, err
	}

	if useFallbackIngress {
		return nil, cs.createFallbackIngress(ctx, cm)
	}

	vsApply := VirtualServiceApplyFromChallengeMeta(cm)

	// This controller is authoritative for these virtualservices so stomp any old versions that exist
	if cs.dryRun {
		log.Info(fmt.Sprintf("dry-run: patching %s.%s %s/%s", *vsApply.Kind, *vsApply.APIVersion, *vsApply.Namespace, *vsApply.Name))
		return nil, nil
	}

	return cs.networkingClient.VirtualServices(challenge.Namespace).Apply(ctx, vsApply, metav1.ApplyOptions{Force: true, FieldManager: "challengesolver"})
}

// shouldUseFallbackIngress checks if the Gateway has the DNS disabled annotation,
// indicating that DNS is not pointing to Istio and we need to use a fallback Ingress.
func (cs *ChallengeSolver) shouldUseFallbackIngress(ctx context.Context, namespacedGateway string) (bool, error) {
	if !cs.fallbackIngress.Enabled {
		return false, nil
	}

	parts := strings.SplitN(namespacedGateway, "/", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid gateway format: %s", namespacedGateway)
	}
	namespace, name := parts[0], parts[1]

	gateway, err := cs.istioClient.NetworkingV1beta1().Gateways(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get gateway %s: %w", namespacedGateway, err)
	}

	if val, ok := gateway.Annotations[cs.fallbackIngress.DNSDisabledAnnotation]; ok && val == "true" {
		return true, nil
	}

	return false, nil
}

// createFallbackIngress creates a Kubernetes Ingress resource to route ACME challenge traffic
// through a non-Istio ingress controller (e.g., Traefik) during migration scenarios.
func (cs *ChallengeSolver) createFallbackIngress(ctx context.Context, cm ChallengeMeta) error {
	log := log.FromContext(ctx)

	ingress := IngressFromChallengeMeta(cm, cs.fallbackIngress.IngressClass)

	if cs.dryRun {
		log.Info(fmt.Sprintf("dry-run: creating Ingress %s/%s", ingress.Namespace, ingress.Name))
		return nil
	}

	log.Info(fmt.Sprintf("Creating fallback Ingress %s/%s for challenge %s", ingress.Namespace, ingress.Name, cm.DNSName))

	_, err := cs.k8sClient.NetworkingV1().Ingresses(ingress.Namespace).Create(ctx, ingress, metav1.CreateOptions{FieldManager: "challengesolver"})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = cs.k8sClient.NetworkingV1().Ingresses(ingress.Namespace).Update(ctx, ingress, metav1.UpdateOptions{FieldManager: "challengesolver"})
		}
	}

	return err
}

// IngressFromChallengeMeta creates a Kubernetes Ingress resource for routing ACME challenge traffic.
func IngressFromChallengeMeta(cm ChallengeMeta, ingressClass string) *networkingv1.Ingress {
	pathTypeExact := networkingv1.PathTypeExact

	return &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cm.Name,
			Namespace: cm.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: acmev1.SchemeGroupVersion.String(),
					Kind:       "Challenge",
					Name:       cm.Name,
					UID:        cm.UID,
				},
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClass,
			Rules: []networkingv1.IngressRule{
				{
					Host: cm.DNSName,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     fmt.Sprintf("/.well-known/acme-challenge/%s", cm.Token),
									PathType: &pathTypeExact,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: cm.Service,
											Port: networkingv1.ServiceBackendPort{
												Number: cm.Port,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
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
