package labels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildLabelSelector(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name=true", InjectSimpleCredentialNameLabelSelector())
}
