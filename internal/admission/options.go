package admission

import corev1listers "k8s.io/client-go/listers/core/v1"

type OptionsFunc func(*GatewayMutationHook)

func WithExternalDNSTarget(target string) OptionsFunc {
	return func(gmh *GatewayMutationHook) {
		if target != "" {
			if gmh.externalDNS == nil {
				gmh.externalDNS = &externalDNSConfig{
					target: target,
				}
			} else {
				gmh.externalDNS.target = target
			}
		}
	}
}

func SetExternalDNS(enabled bool) OptionsFunc {
	return func(gmh *GatewayMutationHook) {
		if gmh.externalDNS == nil {
			gmh.externalDNS = &externalDNSConfig{
				enabled: enabled,
			}
		} else {
			gmh.externalDNS.enabled = enabled
		}
	}
}

func WithNSLister(nsl corev1listers.NamespaceLister) OptionsFunc {
	return func(gmh *GatewayMutationHook) {
		if gmh != nil {
			gmh.nsLister = nsl
		}
	}
}
