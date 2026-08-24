package challengesolver

import (
	"context"
	"fmt"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	apinetv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	netapplymetav1 "istio.io/client-go/pkg/applyconfiguration/meta/v1"
	netapplyv1beta1 "istio.io/client-go/pkg/applyconfiguration/networking/v1beta1"
	networkingv1beta1Client "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1"

	istiov1beta1 "istio.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// VirtualServicePlugin creates Istio VirtualService resources to answer ACME
// HTTP-01 challenges. It is applicable for all gateways that do NOT have the
// ingress-http01 annotation set (normal operation).
type VirtualServicePlugin struct {
	networkingClient networkingv1beta1Client.NetworkingV1beta1Interface
	dryRun           bool
}

// NewVirtualServicePlugin constructs a VirtualServicePlugin.
func NewVirtualServicePlugin(nc networkingv1beta1Client.NetworkingV1beta1Interface, dryRun bool) *VirtualServicePlugin {
	return &VirtualServicePlugin{
		networkingClient: nc,
		dryRun:           dryRun,
	}
}

// Applicable returns true when the hostname is not flagged for ingress-based
// solving. Cache-aware logic is introduced in a later commit; for now this
// always returns true so that the plugin is the default routing path.
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
func virtualServiceApplyFromChallengeMeta(cm ChallengeMeta) *netapplyv1beta1.VirtualServiceApplyConfiguration {
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

// VirtualServiceApplyFromChallengeMeta is the exported form of
// virtualServiceApplyFromChallengeMeta, retained for use by solver.go's
// direct fallback path until the coordinator refactor is complete.
func VirtualServiceApplyFromChallengeMeta(cm ChallengeMeta) *netapplyv1beta1.VirtualServiceApplyConfiguration {
	return virtualServiceApplyFromChallengeMeta(cm)
}
