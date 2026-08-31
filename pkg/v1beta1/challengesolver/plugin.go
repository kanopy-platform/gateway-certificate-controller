package challengesolver

import (
	"context"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ChallengePlugin handles routing-resource creation for a single ACME HTTP-01
// Challenge. The ChallengeSolver coordinator calls Applicable to select the
// first matching plugin, then calls Solve to create the routing resource.
type ChallengePlugin interface {
	// Applicable reports whether this plugin should handle the given challenge.
	Applicable(ctx context.Context, challenge *acmev1.Challenge) bool
	// Solve creates or updates the routing resource that answers the challenge.
	Solve(ctx context.Context, meta ChallengeMeta) error
}

// ChallengeMeta holds the resolved metadata needed by a ChallengePlugin to
// create a routing resource.
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
