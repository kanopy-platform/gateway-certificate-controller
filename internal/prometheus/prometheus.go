package prometheus

import (
	"time"

	certmanager "github.com/cert-manager/cert-manager/pkg/client/listers/certmanager/v1"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/labels"
	klog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var Metrics = New()

type Prometheus struct {
	durationBuckets          []float64
	mutationWebhookLatency   prometheus.Histogram
	managedCertificatesCount prometheus.Gauge
}

func New(opts ...OptionFunc) *Prometheus {
	p := &Prometheus{
		durationBuckets: []float64{0.05, 0.1, 0.2, 0.5, 1},
	}

	for _, opt := range opts {
		opt(p)
	}

	p.mutationWebhookLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "mutation_webhook_duration_seconds",
		Help:    "Duration of admission mutation webhook",
		Buckets: p.durationBuckets,
	})

	p.managedCertificatesCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "managed_certificates_count",
		Help: "Count of controller managed certificates",
	})

	metrics.Registry.MustRegister(p.mutationWebhookLatency)
	metrics.Registry.MustRegister(p.managedCertificatesCount)

	return p
}

// func (p *Prometheus) Handler() http.Handler {
// 	return promhttp.Handler()
// }

func (p *Prometheus) UpdateMutationWebhookLatency(t float64) {
	p.mutationWebhookLatency.Observe(t)
}

func (p *Prometheus) PollManagedCertificatesCount(certLister certmanager.CertificateLister) {
	go func() {
		for {
			certs, err := certLister.List(labels.Everything())
			if err != nil {
				klog.Log.Error(err, "failed to list certificates")
				continue
			}

			p.managedCertificatesCount.Set(float64(len(certs)))
			time.Sleep(time.Second * 30)
		}
	}()
}
