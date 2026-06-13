package challengesolver

type OptionsFunc func(cs *ChallengeSolver)

func WithDryRun(dryrun bool) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.dryRun = dryrun
	}
}

// FallbackIngressConfig holds configuration for creating Ingress resources
// when DNS is not pointing to Istio (e.g., during migration from another ingress controller).
type FallbackIngressConfig struct {
	Enabled             bool
	DNSDisabledAnnotation string
	IngressClass        string
}

func WithFallbackIngress(cfg FallbackIngressConfig) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.fallbackIngress = cfg
	}
}
