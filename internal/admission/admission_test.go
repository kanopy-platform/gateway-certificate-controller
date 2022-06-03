package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

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

	gmh := NewGatewayMutationHook(istiofake.NewSimpleClientset())

	scheme := runtime.NewScheme()
	utilruntime.Must(v1beta1.SchemeBuilder.AddToScheme(scheme))

	decoder, err := admission.NewDecoder(scheme)
	assert.NoError(t, err)
	assert.NoError(t, gmh.InjectDecoder(decoder))

	gateway := &v1beta1.Gateway{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "devops",
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				{
					Tls: &networkingv1beta1.ServerTLSSettings{
						Mode:           networkingv1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: "should-be-replaced",
					},
				},
			},
		},
	}

	gatewayBytes, err := json.Marshal(gateway)
	assert.NoError(t, err)

	tests := []struct {
		description string
		request     admissionv1.AdmissionRequest
		wantAllowed bool
	}{
		{
			description: "Empty AdmissionRequest should be rejected",
			request:     admissionv1.AdmissionRequest{},
			wantAllowed: false,
		},
		{
			description: "Successful AdmissionRequest with Gateway",
			request: admissionv1.AdmissionRequest{
				Object: runtime.RawExtension{
					Raw: gatewayBytes,
				},
			},
			wantAllowed: true,
		},
	}

	for _, test := range tests {
		response := gmh.Handle(context.TODO(), admission.Request{AdmissionRequest: test.request})
		assert.Equal(t, test.wantAllowed, response.Allowed, test.description)
	}
}

func TestCredentialName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description  string
		namespace    string
		name         string
		wantContains string
		wantLen      int
	}{
		{
			description:  "generated credentialName within character limit",
			namespace:    "devops",
			name:         "example-gateway",
			wantContains: "devops-example-gateway-",
			wantLen:      33,
		},
		{
			description: "generated credentialName is truncated",
			namespace:   strings.Repeat("a", 125),
			name:        strings.Repeat("b", 125),
			// some characters from the end of name should be truncated
			wantContains: fmt.Sprintf("%s-%s-", strings.Repeat("a", 125), strings.Repeat("b", 116)),
			wantLen:      secretNameMaxLength,
		},
	}

	for _, test := range tests {
		n := credentialName(context.TODO(), test.namespace, test.name)

		assert.Contains(t, n, test.wantContains, test.description)
		assert.Equal(t, test.wantLen, len(n), test.description)
	}
}

func TestMutate(t *testing.T) {
	t.Parallel()

	gateway := v1beta1.Gateway{
		ObjectMeta: v1.ObjectMeta{
			Name:        "example-gateway",
			Namespace:   "devops",
			Labels:      map[string]string{"v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name": "true"},
			Annotations: map[string]string{},
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

	mutatedGateway := mutate(context.TODO(), gateway.DeepCopy())

	assert.Equal(t, gateway.Spec.Servers[0], mutatedGateway.Spec.Servers[0])
	assert.Equal(t, gateway.Spec.Servers[1], mutatedGateway.Spec.Servers[1])
	assert.Equal(t, gateway.Spec.Servers[2].Tls.Mode, mutatedGateway.Spec.Servers[2].Tls.Mode)
	assert.NotEqual(t, gateway.Spec.Servers[2].Tls.CredentialName, mutatedGateway.Spec.Servers[2].Tls.CredentialName)
}

func TestCanMutateCredentialName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		name        string
		gatewayName string
		want        bool
	}{
		{
			description: "Should not mutate a credential name",
			name:        "ns-gatewayname-ahssbs",
			gatewayName: "gatewayname",
			want:        false,
		},
		{
			description: "Should not mutate credential name with gateway-name contains hyphens",
			name:        "ns-gateway-name-sdfjhdfs",
			gatewayName: "gateway-name",
			want:        false,
		},
		{
			description: "Should not mutate credential name with gateway-name any number of hyphens",
			name:        "ns-gateway-name-long-name-sdfjhdfs",
			gatewayName: "gateway-name-long-name",
			want:        false,
		},
		{
			description: "Should mutate a user set credential name",
			name:        "defaultsecret",
			want:        true,
		},
		{
			description: "Should mutate a user set credential name using a similar format",
			name:        "another-format-similar",
			want:        true,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, canMutateCredentialName(test.name, "ns", test.gatewayName), test.description)
	}
}
