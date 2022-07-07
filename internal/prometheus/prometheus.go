package prometheus

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	managedCertificatesCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "managed_certificates_count",
		Help: "Count of controller managed certificates",
	})
)

func init() {
	metrics.Registry.MustRegister(managedCertificatesCount)
}

func Handler() http.Handler {
	return promhttp.InstrumentMetricHandler(
		metrics.Registry, promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}),
	)
}

func UpdateManagedCertificatesCount(count int) {
	managedCertificatesCount.Set(float64(count))
}
