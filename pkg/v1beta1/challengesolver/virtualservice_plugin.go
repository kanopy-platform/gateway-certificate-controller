package challengesolver

import (
	"context"
	"fmt"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	apinetv1 "istio.io/client-go/pkg/apis/networking/v1"
	netapplymetav1 "istio.io/client-go/pkg/applyconfiguration/meta/v1"
	netapplyv1 "istio.io/client-go/pkg/applyconfiguration/networking/v1"
	networkingv1client "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1"

	istiov1 "istio.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// VirtualServicePlugin creates Istio VirtualService resources to answer ACME
// HTTP-01 challenges. It is always applicable — a VirtualService is created
// for every challenge regardless of whether the gateway also has the
// ingress-http01 annotation set.
type VirtualServicePlugin struct {
	networkingClient networkingv1client.NetworkingV1Interface
	dryRun           bool
}

// NewVirtualServicePlugin constructs a VirtualServicePlugin.
func NewVirtualServicePlugin(nc networkingv1client.NetworkingV1Interface, dryRun bool) *VirtualServicePlugin {
	return &VirtualServicePlugin{
		networkingClient: nc,
		dryRun:           dryRun,
	}
}

// Applicable always returns true. A VirtualService is created for every
// eligible challenge so that the route is in place as soon as DNS cuts over
// to Istio, even if an Ingress is simultaneously being used during the
// migration window.
func (p *VirtualServicePlugin) Applicable(_ context.Context, _ *acmev1.Challenge) bool {
	return true
}

// Solve SSA-applies a VirtualService that routes the ACME challenge path to
// the cert-manager solver Service. The owner reference on the Challenge ensures
// automatic cleanup when cert-manager deletes the Challenge.
func (p *VirtualServicePlugin) Solve(ctx context.Context, meta ChallengeMeta) error {
	log := log.FromContext(ctx)
	vsApply := virtualServiceApplyFromChallengeMeta(meta)

	if p.dryRun {
		log.Info(fmt.Sprintf("dry-run: patching %s.%s %s/%s", *vsApply.Kind, *vsApply.APIVersion, *vsApply.Namespace, *vsApply.Name))
		return nil
	}

	_, err := p.networkingClient.VirtualServices(meta.Namespace).Apply(ctx, vsApply, metav1.ApplyOptions{Force: true, FieldManager: "challengesolver"})
	return err
}

// virtualServiceApplyFromChallengeMeta constructs the SSA apply configuration
// for an Istio VirtualService that routes the ACME challenge token path.
func virtualServiceApplyFromChallengeMeta(cm ChallengeMeta) *netapplyv1.VirtualServiceApplyConfiguration {
	vsAPIVersion := apinetv1.SchemeGroupVersion.String()
	vsKind := "VirtualService"

	vsApply := netapplyv1.VirtualServiceApplyConfiguration{
		ObjectMetaApplyConfiguration: &netapplymetav1.ObjectMetaApplyConfiguration{},
		Spec: &istiov1.VirtualService{
			Hosts:    []string{cm.DNSName},
			Gateways: []string{cm.Gateway},
			Http: []*istiov1.HTTPRoute{
				{
					Name: "solver",
					Match: []*istiov1.HTTPMatchRequest{
						{
							Uri: &istiov1.StringMatch{
								MatchType: &istiov1.StringMatch_Exact{
									Exact: fmt.Sprintf("/.well-known/acme-challenge/%s", cm.Token),
								},
							},
						},
					},
					Route: []*istiov1.HTTPRouteDestination{
						{
							Destination: &istiov1.Destination{
								Host: cm.Service,
								Port: &istiov1.PortSelector{
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
