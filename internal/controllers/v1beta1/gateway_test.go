package v1beta1

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"

	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type reconcilerSpy struct {
	eventHandler
	CleanupCalled int
	Error         bool
}

func setupControllerWithSpy(cs *istiofake.Clientset) (*GatewayController, *reconcilerSpy) {
	eventSpy := &reconcilerSpy{}
	g := NewGatewayController(cs)
	g.events = eventSpy
	return g, eventSpy
}

func (r *reconcilerSpy) Cleanup(ctx context.Context, request reconcile.Request) error {
	r.CleanupCalled++
	if r.Error {
		return fmt.Errorf("mock error")
	}
	return r.eventHandler.Cleanup(ctx, request)
}

func reconcileRequest() reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test",
			Name:      "mygateway",
		},
	}
}

func TestGatewayControllerReconcileNoError(t *testing.T) {
	t.Parallel()
	g := NewGatewayController(istiofake.NewSimpleClientset())
	r, err := g.Reconcile(context.TODO(), reconcile.Request{})
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, r)
}

func TestGatewayReconcile_CallsCleanupOnNotExists(t *testing.T) {
	t.Parallel()
	g, eventSpy := setupControllerWithSpy(istiofake.NewSimpleClientset())

	r, err := g.Reconcile(context.TODO(), reconcileRequest())
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, r)
	assert.Equal(t, 1, eventSpy.CleanupCalled)
}

func TestGatewayReconcile_CleanupErrors(t *testing.T) {
	t.Parallel()
	g, eventSpy := setupControllerWithSpy(istiofake.NewSimpleClientset())
	eventSpy.Error = true
	r, err := g.Reconcile(context.TODO(), reconcileRequest())
	assert.Error(t, err)
	assert.Equal(t, reconcile.Result{Requeue: true}, r)
}
