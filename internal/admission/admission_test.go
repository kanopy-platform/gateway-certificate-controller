package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"
	"github.com/stretchr/testify/assert"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type fakeNSLister struct {
	ns map[string]*corev1.Namespace
}

func (fnsl *fakeNSLister) List(selector labels.Selector) (ret []*corev1.Namespace, err error) {
	ret = []*corev1.Namespace{}

	return ret, err
}

func (fnsl *fakeNSLister) Get(name string) (*corev1.Namespace, error) {

	if fnsl.ns == nil {
		fnsl.ns = map[string]*corev1.Namespace{}
	}

	var e error
	ns, ok := fnsl.ns[name]
	if !ok {
		e = fmt.Errorf("Not Found")
	}
	return ns, e
}

func (fnsl *fakeNSLister) set(ns *corev1.Namespace) {
	if fnsl == nil || ns == nil {
		return
	}

	if fnsl.ns == nil {
		fnsl.ns = map[string]*corev1.Namespace{}
	}

	n := ns.DeepCopy()
	fnsl.ns[n.Name] = n
}

func TestGatewayMutationHook(t *testing.T) {
	t.Parallel()

	nsl := &fakeNSLister{}
	ns := corev1.Namespace{}
	ns.Name = "devops"
	nsl.set(&ns)

	gmh := NewGatewayMutationHook(istiofake.NewSimpleClientset(), nsl)

	scheme := runtime.NewScheme()
	utilruntime.Must(v1beta1.SchemeBuilder.AddToScheme(scheme))

	decoder, err := admission.NewDecoder(scheme)
	assert.NoError(t, err)
	assert.NoError(t, gmh.InjectDecoder(decoder))

	gateway := &v1beta1.Gateway{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "devops",
			Labels: map[string]string{
				v1beta1labels.InjectSimpleCredentialNameLabel: "true",
			},
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				{
					Port: &networkingv1beta1.Port{
						Number: 443,
						Name:   "https",
					},
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
	const portName = "https"
	tests := []struct {
		description string
		namespace   string
		name        string
		want        string
		wantLen     int
	}{
		{
			description: "generated credentialName within character limit",
			namespace:   "devops",
			name:        "example-gateway",
			want:        fmt.Sprintf("devops-example-gateway-%s", portName),
			wantLen:     28,
		},
		{
			description: "generated credentialName is truncated",
			namespace:   strings.Repeat("a", 125),
			name:        strings.Repeat("b", 125),
			// some characters from the end of name should be truncated
			want:    fmt.Sprintf("%s-%s-%s", strings.Repeat("a", 125), strings.Repeat("b", 121), portName),
			wantLen: secretNameMaxLength,
		},
	}

	for _, test := range tests {
		n := credentialName(context.TODO(), test.namespace, test.name, portName)

		assert.Equal(t, n, test.want, test.description)
		assert.Equal(t, test.wantLen, len(n), test.description)
	}
}

func TestMutate(t *testing.T) {
	t.Parallel()

	gateway := v1beta1.Gateway{
		ObjectMeta: v1.ObjectMeta{
			Name:      "example-gateway",
			Namespace: "devops",
			Labels:    map[string]string{v1beta1labels.InjectSimpleCredentialNameLabel: "true"},
			Annotations: map[string]string{
				v1beta1labels.ExternalDNSTargetAnnotationKey:   "there",
				v1beta1labels.ExternalDNSHostnameAnnotationKey: "more,hosts",
			},
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				{
					Tls: nil,
					Port: &networkingv1beta1.Port{
						Number: 80,
						Name:   "http",
					},
				},
				{
					Port: &networkingv1beta1.Port{
						Number: 443,
						Name:   "https",
					},
					Tls: &networkingv1beta1.ServerTLSSettings{
						Mode:           networkingv1beta1.ServerTLSSettings_PASSTHROUGH,
						CredentialName: "should-not-be-mutated",
					},
				},
				{
					Port: &networkingv1beta1.Port{
						Number: 443,
						Name:   "https",
					},
					Tls: &networkingv1beta1.ServerTLSSettings{
						Mode:           networkingv1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: "should-be-mutated",
					},
				},
			},
		},
	}

	eDNS := externalDNSConfig{
		enabled: true,
		target:  "vanity-target",
	}

	ns := corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{"ingress-whitelist": "*.devops.example.com"},
			Name:        "devops",
		},
	}

	mutatedGateway := mutate(context.TODO(), gateway.DeepCopy(), &eDNS, &ns)

	assert.Equal(t, gateway.Spec.Servers[0], mutatedGateway.Spec.Servers[0])
	assert.Equal(t, gateway.Spec.Servers[1], mutatedGateway.Spec.Servers[1])
	assert.Equal(t, gateway.Spec.Servers[2].Tls.Mode, mutatedGateway.Spec.Servers[2].Tls.Mode)
	assert.NotEqual(t, gateway.Spec.Servers[2].Tls.CredentialName, mutatedGateway.Spec.Servers[2].Tls.CredentialName)

	assert.NotNil(t, mutatedGateway.Annotations)
	_, found := mutatedGateway.Annotations[v1beta1labels.ExternalDNSHostnameAnnotationKey]
	assert.False(t, found)
	assert.Equal(t, "vanity-target", mutatedGateway.Annotations[v1beta1labels.ExternalDNSTargetAnnotationKey])

	// Ensure we don't mutate external dns annotations on allowed ns
	ns = corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{"ingress-whitelist": "*"},
			Name:        "devops",
		},
	}

	mutatedGateway = mutate(context.TODO(), gateway.DeepCopy(), &eDNS, &ns)
	assert.NotNil(t, mutatedGateway.Annotations)
	assert.Equal(t, "more,hosts", mutatedGateway.Annotations[v1beta1labels.ExternalDNSHostnameAnnotationKey])
	assert.Equal(t, "there", gateway.Annotations[v1beta1labels.ExternalDNSTargetAnnotationKey])

	// Ensure we mutate external dns labels for gateways without our tls label
	ns = corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{"ingress-whitelist": "*.devops.example.com"},
			Name:        "devops",
		},
	}

	gateway.Labels = map[string]string{}
	mutatedGateway = mutate(context.TODO(), gateway.DeepCopy(), &eDNS, &ns)
	assert.NotNil(t, mutatedGateway.Annotations)
	_, found = mutatedGateway.Annotations[v1beta1labels.ExternalDNSHostnameAnnotationKey]
	assert.False(t, found)
	assert.Equal(t, "vanity-target", mutatedGateway.Annotations[v1beta1labels.ExternalDNSTargetAnnotationKey])
	assert.Equal(t, gateway.Spec.Servers[2].Tls.CredentialName, mutatedGateway.Spec.Servers[2].Tls.CredentialName)

	// ensure we remove the target annotation if no target is set
	eDNS = externalDNSConfig{
		enabled: true,
	}
	mutatedGateway = mutate(context.TODO(), gateway.DeepCopy(), &eDNS, &ns)
	assert.NotNil(t, mutatedGateway.Annotations)
	_, found = mutatedGateway.Annotations[v1beta1labels.ExternalDNSTargetAnnotationKey]
	assert.False(t, found)
}
