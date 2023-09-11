package challengesolver

import (
	acmev1Client "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	networkingv1beta1 "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type OptionsFunc func(*ChallengeSolver)

func WithCoreClient(clientset corev1.CoreV1Interface) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.coreClient = clientset
	}
}

func WithNetorkingClient(clientset networkingv1beta1.NetworkingV1beta1Interface) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.networkingClient = clientset
	}
}

func WithAcmeClient(clientset acmev1Client.AcmeV1Interface) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.acmeClient = clientset
	}
}

func WithGLC(glc *cache.GatewayLookupCache) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.glc = glc
	}
}
