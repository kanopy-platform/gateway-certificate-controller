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
