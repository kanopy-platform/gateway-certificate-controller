package garbagecollection

import (
	"context"
	"testing"

	v1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagerfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	"istio.io/api/networking/v1beta1"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNewGarbageCollectionController(t *testing.T) {
	t.Parallel()

	certmanagerClient := certmanagerfake.NewSimpleClientset()
	istioClient := istiofake.NewSimpleClientset()
	dryRun := true

	want := &GarbageCollectionController{
		name:              "istio-garbage-collection-controller",
		certmanagerClient: certmanagerClient,
		istioClient:       istioClient,
		dryRun:            dryRun,
		managedCerts:      map[string]bool{},
	}

	gc := NewGarbageCollectionController(istioClient, certmanagerClient, WithDryRun(dryRun))
	assert.Equal(t, want, gc)
}

func TestGarbageCollectionControllerReconcile(t *testing.T) {
	t.Parallel()

	certificate := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devops-gateway-123-cert",
			Namespace: "routing",
			Labels:    map[string]string{"v1beta1.kanopy-platform.github.io/istio-cert-controller-managed": "gateway-123.devops"},
		},
	}

	gatewayWithCert := &networkingv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-123",
			Namespace: "devops",
		},
		Spec: v1beta1.Gateway{
			Servers: []*v1beta1.Server{
				{
					Tls: &v1beta1.ServerTLSSettings{
						CredentialName: "devops-gateway-123-diff-cert",
					},
				},
				{
					Tls: &v1beta1.ServerTLSSettings{
						CredentialName: "devops-gateway-123-cert", // should match certificate name
					},
				},
			},
		},
	}

	gatewayWithoutCert := &networkingv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-123",
			Namespace: "devops",
		},
		Spec: v1beta1.Gateway{
			Servers: []*v1beta1.Server{
				{
					Tls: &v1beta1.ServerTLSSettings{
						CredentialName: "devops-gateway-123-diff-cert",
					},
				},
			},
		},
	}

	reconcileRequest := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: certificate.Namespace,
			Name:      certificate.Name,
		},
	}

	tests := []struct {
		description  string
		certs        []*v1.Certificate
		gateways     []*networkingv1beta1.Gateway
		wantError    bool
		wantNumCerts int
	}{
		{
			description:  "Certificate points to existing Gateway, no-op",
			certs:        []*v1.Certificate{certificate},
			gateways:     []*networkingv1beta1.Gateway{gatewayWithCert},
			wantError:    false,
			wantNumCerts: 1,
		},
		{
			description:  "Certificate points to missing Gateway, delete Certificate",
			certs:        []*v1.Certificate{certificate},
			gateways:     []*networkingv1beta1.Gateway{}, // no Gateway
			wantError:    false,
			wantNumCerts: 0,
		},
		{
			description:  "Reconcile called on a Certificate that doesn't exist anymore",
			certs:        []*v1.Certificate{}, // no Certificate
			gateways:     []*networkingv1beta1.Gateway{},
			wantError:    true,
			wantNumCerts: 0,
		},
		{
			description:  "Gateway does not contain Certificate, delete Certificate",
			certs:        []*v1.Certificate{certificate},
			gateways:     []*networkingv1beta1.Gateway{gatewayWithoutCert},
			wantError:    false,
			wantNumCerts: 0,
		},
	}

	for _, test := range tests {
		// setup
		gc := NewGarbageCollectionController(istiofake.NewSimpleClientset(), certmanagerfake.NewSimpleClientset())

		for _, cert := range test.certs {
			_, err := gc.certmanagerClient.CertmanagerV1().Certificates(cert.Namespace).Create(context.TODO(), cert, metav1.CreateOptions{})
			assert.NoError(t, err, test.description)
		}
		for _, gateway := range test.gateways {
			_, err := gc.istioClient.NetworkingV1beta1().Gateways(gateway.Namespace).Create(context.TODO(), gateway, metav1.CreateOptions{})
			assert.NoError(t, err, test.description)
		}

		// test Reconcile
		r, err := gc.Reconcile(context.TODO(), reconcileRequest)

		if test.wantError {
			assert.Error(t, err, test.description)
			assert.Equal(t, reconcile.Result{Requeue: true}, r)
		} else {
			assert.NoError(t, err, test.description)
			assert.Equal(t, reconcile.Result{}, r)
		}

		certs, err := gc.certmanagerClient.CertmanagerV1().Certificates(certificate.Namespace).List(context.TODO(), metav1.ListOptions{})
		assert.NoError(t, err, test.description)
		assert.Equal(t, test.wantNumCerts, len(certs.Items), test.description)
	}
}

func TestUpdateFunc(t *testing.T) {
	t.Parallel()

	q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	event1 := event.UpdateEvent{
		ObjectNew: &v1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cert",
				Namespace: "routing",
			},
		},
	}

	event2 := event.UpdateEvent{
		ObjectNew: &v1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cert-2",
				Namespace: "routing",
			},
		},
	}

	updateFunc(event1, q)
	updateFunc(event2, q)

	assert.Equal(t, 2, q.Len())

	req, _ := q.Get()
	assert.Equal(t, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "routing", Name: "test-cert"}}, req)
	req, _ = q.Get()
	assert.Equal(t, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "routing", Name: "test-cert-2"}}, req)
}

func TestIsCertificateInGatewaySpec(t *testing.T) {
	t.Parallel()

	gateway := &networkingv1beta1.Gateway{
		Spec: v1beta1.Gateway{
			Servers: []*v1beta1.Server{
				{
					Tls: &v1beta1.ServerTLSSettings{
						CredentialName: "some-other-cred",
					},
				},
				{
					Tls: &v1beta1.ServerTLSSettings{
						CredentialName: "devops-gateway-123-https",
					},
				},
			},
		},
	}

	tests := []struct {
		description string
		certificate string
		gateway     *networkingv1beta1.Gateway
		want        bool
	}{
		{
			description: "Certificate exists in Gateway spec",
			certificate: "devops-gateway-123-https",
			gateway:     gateway,
			want:        true,
		},
		{
			description: "Certificate does not exist in Gateway spec",
			certificate: "no-match",
			gateway:     gateway,
			want:        false,
		},
	}

	for _, test := range tests {
		assert.Equal(t, test.want, isCertificateInGatewaySpec(test.certificate, test.gateway), test.description)
	}
}
