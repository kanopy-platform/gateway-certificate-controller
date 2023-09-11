package challengesolver

import (
	certmanagerversionedclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
)

type OptionsFunc func(*ChallengeSolver)

func WithCertManagerClient(cmc certmanagerversionedclient.Interface) OptionsFunc {
	return func(cs *ChallengeSolver) {
		cs.certmanagerClient = cmc
	}
}
