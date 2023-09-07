package challengesolver

import (
	"context"
	"fmt"
	"hash/adler32"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	networkingv1beta1 "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1"

	istiov1beta1 "istio.io/api/networking/v1beta1"

	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type ChallengeSolver struct {
	coreClient       corev1.Corev1Interface
	networkingClient networkingv1beta1.NetworkingV1beta1Interface
	glc              *cache.GatewayLookupCache
}

func (cs *ChallengeSolver) Solve(challenge *acmev1.Challenge) {

	httpDomainHash := fmt.Sprintf("%d", adler32.Checksum([]byte(challenge.Spec.DNSName)))
	tokenHash := fmt.Sprintf("%d", adler32.Checksum([]byte(challenge.Spec.Token)))

	namespacedGateway, ok := glc.Get(challenge.Spec.DNSName)
	if !ok {
		// requeue the request to wait for the lookup cache to populate
		// probably needs backoff
		return
	}

	listOpts := metav1.ListOptions{
		LabelSelector: fmt.Sprinf("%s=%s,%s=%s", acmev1.DomainLabelKey, httpDomainHash, acmev1.TokenLabelKey, tokenHash),
	}

	serviceList, err := cs.coreClient.Services(challenge.Namespace).List(context.TODO, listOpts)
	if err != nil {
		// requeue the request to wait for the service to appear in the api
		return
	}

	if len(serviceList.Items) == 0 {
		// requeue the request to wait for the service to appear in the api
		return
	}
	svc := serviceList.Items[0]

	if len(svc.Spec.Ports) == 0 {
		// this is probably unrecoverable
		return
	}
	port := uint(svc.Spec.Ports[0].Port)
	vs := networkingv1beta1.VirtualService{
		Spec: istiov1beta1.VirtualService{
			Hosts:    []string{challenge.Spec.DNSNam},
			Gateways: []string{namespacedGateway},
			Http: []*istiov1beta1.HTTPRoute{
				{
					Name: "solver",
					Match: []*istiov1beta1.HTTPMatchRequest{
						{
							Uri: *istiov1beta1.StringMatch{
								MatchType: StringMatch_Exact{
									Exact: fmt.Sprintf("/.well-known/acme-challenge/%s", challenge.Token),
								},
							},
						},
					},
					Route: []*istiov1beta1.HTTPRouteDestination{
						{
							Destination: *istiov1beta1.Destination{
								Host: svc.Name,
								Port: *istiov1beta1.PortSelector{
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
	vs.ObjectMeta.OwnerReferences[0] = metav1.OwnerReference{
		APIVersion: acmev1.SchemeGroupVersion.String(),
		Kind:       "Challenge",
		Name:       challenge.Name,
		UID:        challenge.UID,
	}

	_, err := cs.networkingClient.VirtualService(challenge.Namespace).Create(context.TODO, &vs, metav1.CreateOptions)
	if err != nil {
		// requeue the request to wait for the service to appear in the api
		return

	}
	return
}
