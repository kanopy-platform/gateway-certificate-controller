package v1beta1

import (
	"context"
	"testing"

	v1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagerfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	}

	gc := NewGarbageCollectionController(certmanagerClient, istioClient).WithDryRun(dryRun)
	assert.Equal(t, want, gc)
}

func TestGarbageCollectionControllerReconcile(t *testing.T) {
	t.Parallel()

	testCertificate := &v1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cert",
			Namespace: "routing",
			Labels:    map[string]string{"v1beta1.kanopy-platform.github.io/istio-cert-controller-managed": "test-gateway.test-ns"},
		},
	}

	testGateway := &networkingv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "test-ns",
		},
	}

	tests := []struct {
		description      string
		certs            []runtime.Object
		gateways         []runtime.Object
		reconcileRequest reconcile.Request
		wantError        bool
	}{
		{
			description: "Certificate points to existing Gateway, no-op",
			certs:       []runtime.Object{testCertificate},
			gateways:    []runtime.Object{testGateway},
			reconcileRequest: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: testCertificate.Namespace,
					Name:      testCertificate.Name,
				},
			},
			wantError: false,
		},
		{
			description: "Certificate points to missing Gateway, delete Gateway",
			certs:       []runtime.Object{testCertificate},
			gateways:    []runtime.Object{}, // no Gateway
			reconcileRequest: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: testCertificate.Namespace,
					Name:      testCertificate.Name,
				},
			},
			wantError: false,
		},
		{
			description: "Reconcile called on a Certificate that doesn't exist anymore",
			certs:       []runtime.Object{}, // no Certificate
			gateways:    []runtime.Object{testGateway},
			reconcileRequest: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: testCertificate.Namespace,
					Name:      testCertificate.Name,
				},
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		gc := NewGarbageCollectionController(certmanagerfake.NewSimpleClientset(test.certs...), istiofake.NewSimpleClientset(test.gateways...))
		r, err := gc.Reconcile(context.TODO(), test.reconcileRequest)

		if test.wantError {
			assert.Error(t, err, test.description)
		} else {
			assert.NoError(t, err, test.description)
		}

		assert.Equal(t, reconcile.Result{}, r)
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
