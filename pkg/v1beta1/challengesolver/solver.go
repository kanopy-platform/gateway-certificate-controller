package challengesolver

import (
	"context"
	"fmt"
	"hash/adler32"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	acmev1Client "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	networkingv1beta1Client "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1"

	istiov1beta1 "istio.io/api/networking/v1beta1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
)

type ChallengeSolver struct {
	coreClient       corev1.CoreV1Interface
	networkingClient networkingv1beta1Client.NetworkingV1beta1Interface
	acmeClient       acmev1Client.AcmeV1Interface
	glc              *cache.GatewayLookupCache
}

func NewChallengeSolver(cc corev1.CoreV1Interface, nc networkingv1beta1Client.NetworkingV1beta1Interface, ac acmev1Client.AcmeV1Interface, glc *cache.GatewayLookupCache, opts ...OptionsFunc) *ChallengeSolver {
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
			return reconcile.Result{}, nil // garbage collection will handle
		}

		log.Error(err, "Error reconciling challenge, requeued")
		return reconcile.Result{
			Requeue: true,
		}, err
	}

	err = cs.Solve(ctx, challenge)
	if err != nil {
		//TODO type errors as recoverable or not
		return reconcile.Result{
			Requeue: true,
		}, err
	}

	return reconcile.Result{}, nil
}

func (cs *ChallengeSolver) Solve(ctx context.Context, challenge *acmev1.Challenge) error {
	log := log.FromContext(ctx)
	log.V(1).Info("Debug")

	httpDomainHash := fmt.Sprintf("%d", adler32.Checksum([]byte(challenge.Spec.DNSName)))
	tokenHash := fmt.Sprintf("%d", adler32.Checksum([]byte(challenge.Spec.Token)))

	namespacedGateway, ok := cs.glc.Get(challenge.Spec.DNSName)
	if !ok {
		// requeue the request to wait for the lookup cache to populate
		// probably needs backoff
		return fmt.Errorf("Host %s: Gateway not found.", challenge.Spec.DNSName)
	}
	log.V(1).Info(fmt.Sprintf("Debug: gateway found %s", namespacedGateway))

	listOpts := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s", acmev1.DomainLabelKey, httpDomainHash, acmev1.TokenLabelKey, tokenHash),
	}

	serviceList, err := cs.coreClient.Services(challenge.Namespace).List(context.TODO(), listOpts)
	if err != nil {
		// requeue the request to wait for the service to appear in the api
		return err
	}

	if len(serviceList.Items) == 0 {
		// requeue the request to wait for the service to appear in the api
		return fmt.Errorf("No service matched selector: %s", fmt.Sprintf("%s=%s,%s=%s", acmev1.DomainLabelKey, httpDomainHash, acmev1.TokenLabelKey, tokenHash))
	}
	svc := serviceList.Items[0]

	if len(svc.Spec.Ports) == 0 {
		// this is probably unrecoverable
		return fmt.Errorf("Service: %s, missing port definition", svc.Name)
	}
	port := uint32(svc.Spec.Ports[0].Port)
	vs := networkingv1beta1.VirtualService{
		Spec: istiov1beta1.VirtualService{
			Hosts:    []string{challenge.Spec.DNSName},
			Gateways: []string{namespacedGateway},
			Http: []*istiov1beta1.HTTPRoute{
				{
					Name: "solver",
					Match: []*istiov1beta1.HTTPMatchRequest{
						{
							Uri: &istiov1beta1.StringMatch{
								MatchType: &istiov1beta1.StringMatch_Exact{
									Exact: fmt.Sprintf("/.well-known/acme-challenge/%s", challenge.Spec.Token),
								},
							},
						},
					},
					Route: []*istiov1beta1.HTTPRouteDestination{
						{
							Destination: &istiov1beta1.Destination{
								Host: svc.Name,
								Port: &istiov1beta1.PortSelector{
									Number: port,
								},
							},
						},
					},
				},
			},
		},
	}

	vs.Name = fmt.Sprintf("%s", challenge.Name)
	vs.Namespace = challenge.Namespace
	vs.ObjectMeta.OwnerReferences = append(vs.ObjectMeta.OwnerReferences, metav1.OwnerReference{
		APIVersion: acmev1.SchemeGroupVersion.String(),
		Kind:       "Challenge",
		Name:       challenge.Name,
		UID:        challenge.UID,
	})

	vsb, _ := yaml.Marshal(vs)
	fmt.Println(string(vsb))

	_, err = cs.networkingClient.VirtualServices(challenge.Namespace).Create(context.TODO(), &vs, metav1.CreateOptions{})
	if err != nil {
		// requeue the request to wait for the service to appear in the api
		return err

	}
	return nil
}
