package labels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInjectSimpleCredentialNameLabel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name", InjectSimpleCredentialNameLabel)
}

func TestInjectSimpleCredentialNameLabelSelector(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name=true", InjectSimpleCredentialNameLabelSelector())
}

func TestIssuerAnnotation(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "v1beta1.kanopy-platform.github.io/istio-cert-controller-issuer", IssuerAnnotation)
}

func TestManagedLabel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "v1beta1.kanopy-platform.github.io/istio-cert-controller-managed", ManagedLabel)
}

func TestManagedLabelSelector(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "v1beta1.kanopy-platform.github.io/istio-cert-controller-managed", ManagedLabelSelector())
}

func TestParseManagedLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input         string
		wantGateway   string
		wantNamespace string
	}{
		{
			input:         "",
			wantGateway:   "",
			wantNamespace: "",
		},
		{
			input:         ".",
			wantGateway:   "",
			wantNamespace: "",
		},
		{
			input:         ".test-ns",
			wantGateway:   "",
			wantNamespace: "test-ns",
		},
		{
			input:         "test-gateway.test-ns",
			wantGateway:   "test-gateway",
			wantNamespace: "test-ns",
		},
	}

	for _, test := range tests {
		g, ns := ParseManagedLabel(test.input)
		assert.Equal(t, test.wantGateway, g, test.input)
		assert.Equal(t, test.wantNamespace, ns, test.input)
	}
}
