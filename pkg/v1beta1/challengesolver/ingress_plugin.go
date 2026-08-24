package challengesolver

import (
	"context"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
)

// IngressPlugin creates Kubernetes Ingress resources to answer ACME HTTP-01
// challenges through the legacy traefik ingress controller. It is applicable
// only when the gateway carries the ingress-http01 annotation, signalling that
// DNS still resolves to the legacy ingress during a vanity-hostname migration.
// The full implementation is introduced in a subsequent commit; this stub
// satisfies the ChallengePlugin interface so the coordinator tests can be
// written up front.
type IngressPlugin struct{}

// NewIngressPlugin constructs an IngressPlugin stub.
func NewIngressPlugin() *IngressPlugin {
	return &IngressPlugin{}
}

// Applicable returns true when the hostname is flagged for ingress-based
// solving. The stub always returns false; cache-aware logic is added later.
func (p *IngressPlugin) Applicable(_ context.Context, _ *acmev1.Challenge) bool {
	return false
}

// Solve creates the Ingress routing resource. The stub is a no-op; the real
// implementation is introduced in a subsequent commit.
func (p *IngressPlugin) Solve(_ context.Context, _ ChallengeMeta) error {
	return nil
}
