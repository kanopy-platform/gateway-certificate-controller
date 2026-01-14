package orderwatcher

type OptionsFunc func(*OrderWatcher)

func WithDryRun(dryrun bool) OptionsFunc {
	return func(ow *OrderWatcher) {
		ow.dryRun = dryrun
	}
}

// EventConfig holds the configurable event reason strings.
type EventConfig struct {
	CertificateReadyReason  string
	CertificateFailedReason string
}

func WithEventConfig(cfg EventConfig) OptionsFunc {
	return func(ow *OrderWatcher) {
		if cfg.CertificateReadyReason != "" {
			ow.eventConfig.CertificateReadyReason = cfg.CertificateReadyReason
		}
		if cfg.CertificateFailedReason != "" {
			ow.eventConfig.CertificateFailedReason = cfg.CertificateFailedReason
		}
	}
}
