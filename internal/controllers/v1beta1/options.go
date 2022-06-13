package v1beta1

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
