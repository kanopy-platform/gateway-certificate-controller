package prometheus

type OptionFunc func(*Prometheus)

func WithDurationBuckets(buckets ...float64) OptionFunc {
	return func(m *Prometheus) {
		m.durationBuckets = buckets
	}
}
