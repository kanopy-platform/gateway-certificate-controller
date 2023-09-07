package challengesolver_test

import (
	"testing"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"

	"github.com/stretchr/testify/assert"

	certmanagerfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
)

func TestStub(t *testing.T) {
	t.Parallel()

	glc := cache.New()

	assert.NotNil(t, glc)

}

type testHelper struct {
	ics *istiofake.Clientset
	ccs *certmanagerfake.Clientset
}
