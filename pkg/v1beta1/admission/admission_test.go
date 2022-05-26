package admission

import (
	"context"
	"fmt"
	"testing"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/random"
	"github.com/stretchr/testify/assert"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestGatewayMutationHook(t *testing.T) {
	t.Parallel()

	g := NewGatewayMutationHook(istiofake.NewSimpleClientset())

	scheme := runtime.NewScheme()
	utilruntime.Must(v1beta1.SchemeBuilder.AddToScheme(scheme))

	decoder, err := admission.NewDecoder(scheme)
	assert.NoError(t, err)
	assert.NoError(t, g.InjectDecoder(decoder))

	response := g.Handle(context.TODO(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{},
	})

	// Empty AdmissionRequest will be rejected
	assert.False(t, response.Allowed)
}

func TestCredentialName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		namespace   string
		name        string
	}{
		{
			description: "namespace-name-random within character limit",
			namespace:   "devops",
			name:        "example-gateway",
		},
		{
			description: "random suffix is truncated",
			namespace:   random.SecureString(125),
			name:        random.SecureString(125),
		},
	}

	for _, test := range tests {
		n := credentialName(test.namespace, test.name)
		assert.Contains(t, n, fmt.Sprintf("%s-%s-", test.namespace, test.name), test.description)
		assert.True(t, len(n) <= secretNameMaxLength, test.description)
	}
}

func TestMutate(t *testing.T) {
	t.Parallel()

	gateway := v1beta1.Gateway{
		ObjectMeta: v1.ObjectMeta{
			Name:      "example-gateway",
			Namespace: "devops",
			Labels:    map[string]string{"v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name": "true"},
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				{
					Tls: nil,
				},
				{
					Tls: &networkingv1beta1.ServerTLSSettings{
						Mode:           networkingv1beta1.ServerTLSSettings_PASSTHROUGH,
						CredentialName: "should-not-be-mutated",
					},
				},
				{
					Tls: &networkingv1beta1.ServerTLSSettings{
						Mode:           networkingv1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: "should-be-mutated",
					},
				},
			},
		},
	}

	mutatedGateway := gateway.DeepCopy()
	mutate(mutatedGateway)

	assert.Equal(t, gateway.Spec.Servers[0], mutatedGateway.Spec.Servers[0])
	assert.Equal(t, gateway.Spec.Servers[1], mutatedGateway.Spec.Servers[1])
	assert.Equal(t, gateway.Spec.Servers[2].Tls.Mode, mutatedGateway.Spec.Servers[2].Tls.Mode)
	assert.NotEqual(t, gateway.Spec.Servers[2].Tls.CredentialName, mutatedGateway.Spec.Servers[2].Tls.CredentialName)
}
