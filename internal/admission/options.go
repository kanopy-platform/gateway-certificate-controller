package admission

import (
	corev1listers "k8s.io/client-go/listers/core/v1"
)

type OptionsFunc func(*GatewayMutationHook)

func WithExternalDNSConfig(edc *ExternalDNSConfig) OptionsFunc {
	return func(gmh *GatewayMutationHook) {
		gmh.externalDNS = edc
	}
}

func WithNSLister(nsl corev1listers.NamespaceLister) OptionsFunc {
	return func(gmh *GatewayMutationHook) {
		gmh.nsLister = nsl
	}
}
