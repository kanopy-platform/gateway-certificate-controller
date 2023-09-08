package challengesolver

import (
	"context"
	"fmt"
	"hash/adler32"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	acmev1Client "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"

	apinetv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	netapplymetav1 "istio.io/client-go/pkg/applyconfiguration/meta/v1"
	netapplyv1beta1 "istio.io/client-go/pkg/applyconfiguration/networking/v1beta1"
	networkingv1beta1Client "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1"

	istiov1beta1 "istio.io/api/networking/v1beta1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ChallengeSolver struct {
	coreClient       corev1.CoreV1Interface
	networkingClient networkingv1beta1Client.NetworkingV1beta1Interface
	acmeClient       acmev1Client.AcmeV1Interface
	glc              *cache.GatewayLookupCache
}

func NewChallengeSolver(cc corev1.CoreV1Interface, nc networkingv1beta1Client.NetworkingV1beta1Interface, ac acmev1Client.AcmeV1Interface, glc *cache.GatewayLookupCache) *ChallengeSolver {
	return &ChallengeSolver{
		coreClient:       cc,
		networkingClient: nc,
		acmeClient:       ac,
		glc:              glc,
	}
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
		return nil, fmt.Errorf("Host %s: Gateway not found.", challenge.Spec.DNSName)
	}
	log.V(1).Info(fmt.Sprintf("Debug: gateway found %s", namespacedGateway))

	listOpts := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s", acmev1.DomainLabelKey, httpDomainHash, acmev1.TokenLabelKey, tokenHash),
	}

	serviceList, err := cs.coreClient.Services(challenge.Namespace).List(context.TODO(), listOpts)
	if err != nil {
		// requeue the request to wait for the service to appear in the api
		return nil, err
	}

	if len(serviceList.Items) == 0 {
		// requeue the request to wait for the service to appear in the api
		return nil, fmt.Errorf("No service matched selector: %s", fmt.Sprintf("%s=%s,%s=%s", acmev1.DomainLabelKey, httpDomainHash, acmev1.TokenLabelKey, tokenHash))
	}
	svc := serviceList.Items[0]

	if len(svc.Spec.Ports) == 0 {
		// this is probably unrecoverable
		return nil, fmt.Errorf("Service: %s, missing port definition", svc.Name)
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
	return cs.networkingClient.VirtualServices(challenge.Namespace).Apply(context.TODO(), vsApply, metav1.ApplyOptions{Force: true})
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
