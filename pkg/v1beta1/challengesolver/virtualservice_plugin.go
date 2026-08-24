package challengesolver

import (
	"context"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
)

// VirtualServicePlugin creates Istio VirtualService resources to answer ACME
// HTTP-01 challenges. It is applicable for all gateways that do NOT have the
// ingress-http01 annotation set (normal operation). The full implementation is
// introduced in a subsequent commit; this stub satisfies the ChallengePlugin
// interface so the coordinator tests can be written up front.
type VirtualServicePlugin struct{}

// NewVirtualServicePlugin constructs a VirtualServicePlugin stub.
func NewVirtualServicePlugin() *VirtualServicePlugin {
	return &VirtualServicePlugin{}
}

// Applicable returns true when the hostname is not flagged for ingress-based
// solving. The stub always returns true; cache-aware logic is added later.
func (p *VirtualServicePlugin) Applicable(_ context.Context, _ *acmev1.Challenge) bool {
	return true
}

// Solve creates the VirtualService routing resource. The stub is a no-op;
// the real implementation is introduced in a subsequent commit.
func (p *VirtualServicePlugin) Solve(_ context.Context, _ ChallengeMeta) error {
	return nil
}
