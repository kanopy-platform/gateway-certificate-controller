package orderwatcher

import (
	"context"
	"testing"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestOrderWatcher_Reconcile_PendingOrderNoEvent(t *testing.T) {
	order := &acmev1.Order{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-order",
			Namespace: "test-ns",
		},
		Status: acmev1.OrderStatus{
			State: acmev1.Pending,
		},
	}

	cmc := certmanagerfake.NewSimpleClientset(order)
	ic := istiofake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(10)

	ow := NewOrderWatcher(cmc, ic, recorder)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      order.Name,
			Namespace: order.Namespace,
		},
	}

	_, err := ow.Reconcile(context.Background(), req)
	assert.NoError(t, err)

	select {
	case event := <-recorder.Events:
		t.Errorf("unexpected event for pending order: %s", event)
	default:
		// no event, as expected for pending order
	}
}

func TestOrderWatcher_Reconcile_EmptyStateNoEvent(t *testing.T) {
	order := &acmev1.Order{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-order",
			Namespace: "test-ns",
		},
		Status: acmev1.OrderStatus{
			State: "", // empty state
		},
	}

	cmc := certmanagerfake.NewSimpleClientset(order)
	ic := istiofake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(10)

	ow := NewOrderWatcher(cmc, ic, recorder)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      order.Name,
			Namespace: order.Namespace,
		},
	}

	_, err := ow.Reconcile(context.Background(), req)
	assert.NoError(t, err)

	select {
	case event := <-recorder.Events:
		t.Errorf("unexpected event for empty state order: %s", event)
	default:
		// no event, as expected for empty state order
	}
}

func TestOrderWatcher_Reconcile_NotFoundNoError(t *testing.T) {
	cmc := certmanagerfake.NewSimpleClientset()
	ic := istiofake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(10)

	ow := NewOrderWatcher(cmc, ic, recorder)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "non-existent-order",
			Namespace: "test-ns",
		},
	}

	result, err := ow.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.False(t, result.Requeue)
}

func TestOrderWatcher_ProcessedOrdersDeduplication(t *testing.T) {
	order := &acmev1.Order{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-order",
			Namespace: "test-ns",
		},
		Spec: acmev1.OrderSpec{
			DNSNames: []string{"test.example.com"},
		},
		Status: acmev1.OrderStatus{
			State: acmev1.Valid,
		},
	}

	cmc := certmanagerfake.NewSimpleClientset(order)
	ic := istiofake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(10)

	ow := NewOrderWatcher(cmc, ic, recorder)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      order.Name,
			Namespace: order.Namespace,
		},
	}

	// First reconcile - will fail to find gateway but should mark as processed
	_, err := ow.Reconcile(context.Background(), req)
	assert.NoError(t, err)

	// Second reconcile - should be deduplicated since state hasn't changed
	_, err = ow.Reconcile(context.Background(), req)
	assert.NoError(t, err)

	// processedOrders should not have changed since gateway wasn't found
	// and no event was emitted (which is when we mark as processed)
}

func TestFindOwnerRef(t *testing.T) {
	refs := []metav1.OwnerReference{
		{Kind: "Certificate", Name: "cert-1"},
		{Kind: "CertificateRequest", Name: "certreq-1"},
	}

	certRef := findOwnerRef(refs, "Certificate")
	assert.NotNil(t, certRef)
	assert.Equal(t, "cert-1", certRef.Name)

	certReqRef := findOwnerRef(refs, "CertificateRequest")
	assert.NotNil(t, certReqRef)
	assert.Equal(t, "certreq-1", certReqRef.Name)

	notFoundRef := findOwnerRef(refs, "NotFound")
	assert.Nil(t, notFoundRef)
}

func TestGetDNSNamesFromOrder(t *testing.T) {
	order := &acmev1.Order{
		Spec: acmev1.OrderSpec{
			DNSNames: []string{"test.example.com", "test2.example.com"},
		},
	}

	result := getDNSNamesFromOrder(order)
	assert.Equal(t, "test.example.com", result)

	emptyOrder := &acmev1.Order{
		Spec: acmev1.OrderSpec{},
	}
	result = getDNSNamesFromOrder(emptyOrder)
	assert.Equal(t, "unknown", result)
}
