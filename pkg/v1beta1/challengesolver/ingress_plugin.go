package challengesolver

import (
	"context"
	"fmt"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	networkingv1 "k8s.io/api/networking/v1"
	applyconfigmetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	applyconfignetworkingv1 "k8s.io/client-go/applyconfigurations/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// IngressPlugin creates Kubernetes Ingress resources to answer ACME HTTP-01
// challenges through the legacy traefik ingress controller. It is applicable
// only when the gateway carries the ingress-http01 annotation, signalling that
// DNS still resolves to the legacy ingress during a vanity-hostname migration.
type IngressPlugin struct {
	runtimeClient client.Client
	glc           *cache.GatewayLookupCache
	ingressClass  string
	dryRun        bool
}

// NewIngressPlugin constructs an IngressPlugin.
func NewIngressPlugin(c client.Client, glc *cache.GatewayLookupCache, ingressClass string, dryRun bool) *IngressPlugin {
	return &IngressPlugin{
		runtimeClient: c,
		glc:           glc,
		ingressClass:  ingressClass,
		dryRun:        dryRun,
	}
}

// Applicable returns true when the gateway for this hostname has the
// ingress-http01 annotation set to "true".
func (p *IngressPlugin) Applicable(_ context.Context, challenge *acmev1.Challenge) bool {
	if p.glc == nil {
		return false
	}
	return p.glc.GetIngressSolver(challenge.Spec.DNSName)
}

// Solve SSA-applies a networking.k8s.io/v1 Ingress that routes the ACME
// challenge token path to the cert-manager solver Service. Both
// spec.ingressClassName and the legacy kubernetes.io/ingress.class annotation
// are set so that the Ingress is recognised by controllers that support either
// convention. The owner reference on the Challenge ensures automatic cleanup
// when cert-manager deletes the Challenge.
func (p *IngressPlugin) Solve(ctx context.Context, meta ChallengeMeta) error {
	log := log.FromContext(ctx)

	if p.dryRun {
		log.Info(fmt.Sprintf("dry-run: applying ingress %s/%s for challenge %s", meta.Namespace, meta.Name, meta.DNSName))
		return nil
	}

	challengePath := fmt.Sprintf("/.well-known/acme-challenge/%s", meta.Token)
	pathType := networkingv1.PathTypeExact
	apiVersion := acmev1.SchemeGroupVersion.String()

	ingress := applyconfignetworkingv1.Ingress(meta.Name, meta.Namespace).
		WithAnnotations(map[string]string{
			// kubernetes.io/ingress.class is the legacy annotation used by
			// ingress controllers that predate spec.ingressClassName (k8s 1.18).
			// Both fields are set so the Ingress is recognised by either convention.
			"kubernetes.io/ingress.class": p.ingressClass,
		}).
		WithOwnerReferences(
			applyconfigmetav1.OwnerReference().
				WithAPIVersion(apiVersion).
				WithKind("Challenge").
				WithName(meta.Name).
				WithUID(meta.UID).
				WithController(true).
				WithBlockOwnerDeletion(true),
		).
		WithSpec(
			applyconfignetworkingv1.IngressSpec().
				WithIngressClassName(p.ingressClass).
				WithRules(
					applyconfignetworkingv1.IngressRule().
						WithHost(meta.DNSName).
						WithHTTP(
							applyconfignetworkingv1.HTTPIngressRuleValue().
								WithPaths(
									applyconfignetworkingv1.HTTPIngressPath().
										WithPath(challengePath).
										WithPathType(pathType).
										WithBackend(
											applyconfignetworkingv1.IngressBackend().
												WithService(
													applyconfignetworkingv1.IngressServiceBackend().
														WithName(meta.Service).
														WithPort(
															applyconfignetworkingv1.ServiceBackendPort().
																WithNumber(meta.Port),
														),
												),
										),
								),
						),
				),
		)

	return p.runtimeClient.Apply(ctx, ingress, client.FieldOwner("challengesolver"), client.ForceOwnership)
}
