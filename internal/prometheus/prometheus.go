package prometheus

import (
	"net/http"
	"time"

	certmanager "github.com/cert-manager/cert-manager/pkg/client/listers/certmanager/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/labels"
	klog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	mutationWebhookLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "mutation_webhook_duration_seconds",
		Help:    "Duration of admission mutation webhook",
		Buckets: []float64{0.05, 0.1, 0.2, 0.5, 1},
	})

	managedCertificatesCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "managed_certificates_count",
		Help: "Count of controller managed certificates",
	})
)

func init() {
	metrics.Registry.MustRegister(mutationWebhookLatency)
	metrics.Registry.MustRegister(managedCertificatesCount)
}

func Handler() http.Handler {
	return promhttp.InstrumentMetricHandler(
		metrics.Registry, promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}),
	)
}

func UpdateMutationWebhookLatency(t float64) {
	mutationWebhookLatency.Observe(t)
}

func PollManagedCertificatesCount(certLister certmanager.CertificateLister) {
	go func() {
		for {
			// certLister already has filtered for the managed label
			certs, err := certLister.List(labels.Everything())
			if err != nil {
				klog.Log.Error(err, "failed to list certificates")
				continue
			}

			managedCertificatesCount.Set(float64(len(certs)))
			time.Sleep(time.Second * 30)
		}
	}()
}
