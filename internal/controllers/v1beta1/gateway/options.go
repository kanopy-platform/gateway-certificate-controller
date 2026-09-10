package gateway

import (
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
)

type OptionsFunc func(*GatewayController)

func WithCertificateNamespace(namespace string) OptionsFunc {
	return func(gc *GatewayController) {
		if namespace != "" {
			gc.certificateNamespace = namespace
		}
	}
}

func WithDefaultClusterIssuer(issuer string) OptionsFunc {
	return func(gc *GatewayController) {
		gc.clusterIssuer = issuer
	}
}

func WithDryRun(dryrun bool) OptionsFunc {
	return func(gc *GatewayController) {
		gc.dryRun = dryrun
	}
}

func WithGatewayLookupCache(glc *cache.GatewayLookupCache) OptionsFunc {
	return func(gc *GatewayController) {
		gc.gatewayLookupCache = glc
	}
}

func WithHTTPSolverLabel(l string) OptionsFunc {
	return func(gc *GatewayController) {
		gc.httpSolverLabel = l
	}
}

// WithIngressHTTPSolverLabel sets the label key stamped on Certificates when
// the gateway carries the ingress-http01 annotation. The label makes
// migration-mode certificates queryable:
//
//	kubectl get certificates -n cert-manager -l <label>=true
func WithIngressHTTPSolverLabel(l string) OptionsFunc {
	return func(gc *GatewayController) {
		gc.ingressSolverLabel = l
	}
}
