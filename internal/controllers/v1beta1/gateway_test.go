package v1beta1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestGatewayControllerReconcileNoError(t *testing.T) {
	t.Parallel()

	g := NewGatewayController(istiofake.NewSimpleClientset())

	r, err := g.Reconcile(context.TODO(), reconcile.Request{})
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, r)
}
