package challengesolver_test

import (
	"context"
	"fmt"
	"hash/adler32"
	"testing"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/challengesolver"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stretchr/testify/assert"

	certmanagerfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	acmefake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1/fake"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	networkingv1beta1fake "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type testHelper struct {
	ics *istiofake.Clientset
	ccs *certmanagerfake.Clientset
	scs *fakeServiceLister
	glc *cache.GatewayLookupCache
	kcs *k8sfake.Clientset
}

func (th *testHelper) newTestSolver() *challengesolver.ChallengeSolver {
	return challengesolver.NewChallengeSolver(th.scs, th.ics, th.ccs, th.kcs, th.glc)
}

func TestChallengeSolver(t *testing.T) {
	for _, test := range []struct {
		name           string
		challenge      *acmev1.Challenge
		service        *corev1.Service
		virtualService *networkingv1beta1.VirtualService
		gatewayName    string
		pass           bool
		validateVS     bool
		noRequeue      bool
	}{
		{
			name:      "no challenge",
			pass:      true,
			noRequeue: true,
		},
		{
			name:      "No Gateway",
			challenge: getChallenge("noservice", "example", "noservice.com"),
			pass:      false,
		},
		{
			name:      "No Gateway",
			challenge: getChallenge("noservice", "example", "noservice.com"),
			pass:      false,
		},
		{
			name:        "No Service",
			challenge:   getChallenge("noservice", "example", "noservice.com"),
			gatewayName: "gateway",
			pass:        false,
		},
		{
			name:        "No Service Port",
			challenge:   getChallenge("noservice", "example", "noservice.com"),
			gatewayName: "gateway",
			service:     getService("noportservice", "example", "noportservice.com", 0),
			pass:        false,
		},
		{
			name:        "Service",
			challenge:   getChallenge("service", "example", "service.com"),
			gatewayName: "gateway",
			service:     getService("service", "example", "service.com", 8888),
			pass:        true,
			validateVS:  true,
			noRequeue:   true,
		},
	} {
		th := testHelper{
			ics: istiofake.NewSimpleClientset(),
			ccs: certmanagerfake.NewSimpleClientset(),
			scs: &fakeServiceLister{Service: test.service},
			glc: cache.New(),
			kcs: k8sfake.NewSimpleClientset(),
		}

		th.ccs.AcmeV1().(*acmefake.FakeAcmeV1).PrependReactor(
			"get",
			"challenges",
			getChallengeFunc(test.challenge))

		if test.challenge != nil {

			if test.gatewayName != "" {
				th.glc.Add(fmt.Sprintf("%s/%s", test.challenge.Namespace, test.gatewayName), test.challenge.Spec.DNSName)
			}

			vs := networkingv1beta1.VirtualService{}
			vs.Name = test.challenge.Name
			vs.Namespace = test.challenge.Namespace
			th.ics.NetworkingV1beta1().(*networkingv1beta1fake.FakeNetworkingV1beta1).PrependReactor(
				"patch",
				"virtualservices",
				func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true,
						&vs,
						nil
				})

		}

		cs := th.newTestSolver()

		out, err := cs.Solve(context.Background(), test.challenge)

		if test.pass {
			assert.NoError(t, err, test.name)

			if test.validateVS {
				assert.NotNil(t, out, test.name)
			} else {
				assert.Nil(t, out, test.name)
			}
		} else {
			assert.Error(t, err, test.name)
			assert.Nil(t, out, test.name)

		}

		if test.challenge != nil {
			resp, err := cs.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: test.challenge.Namespace, Name: test.challenge.Name}})
			if test.pass {
				assert.NoError(t, err, test.name)
			}

			if test.noRequeue {
				assert.False(t, resp.Requeue, test.name)
			} else {
				assert.True(t, resp.Requeue, test.name)
			}
		}

	}
}

func getChallenge(name, namespace, dnsName string) *acmev1.Challenge {
	return &acmev1.Challenge{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "12345",
		},
		Spec: acmev1.ChallengeSpec{
			Token:   "token",
			DNSName: dnsName,
		},
	}
}

func getChallengeFunc(c *acmev1.Challenge) k8stesting.ReactionFunc {
	return func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, c, nil
	}
}

func getService(name, namespace, dnsName string, port int) *corev1.Service {
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"acme.cert-manager.io/http-domain": fmt.Sprint(adler32.Checksum([]byte(dnsName))),
				"acme.cert-manager.io/http-token":  fmt.Sprint(adler32.Checksum([]byte("token"))),
			},
		},
	}
	if port != 0 {
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{Port: int32(port)})
	}
	return &svc
}

type fakeServiceLister struct {
	Service *corev1.Service
}

func (fsl *fakeServiceLister) List(selector labels.Selector) ([]*corev1.Service, error) {
	sl := []*corev1.Service{}
	if fsl.Service != nil {
		sl = append(sl, fsl.Service)
	}
	return sl, nil
}

func (fsl *fakeServiceLister) Services(namespace string) corev1listers.ServiceNamespaceLister {
	return &stub{}
}

type stub struct{}

func (s *stub) List(selector labels.Selector) ([]*corev1.Service, error) {
	return []*corev1.Service{}, nil
}

func (s *stub) Get(name string) (*corev1.Service, error) {
	return nil, nil
}

func TestFallbackIngress(t *testing.T) {
	const (
		dnsDisabledAnnotation = "test.example.com/dns-disabled"
		ingressClass          = "traefik"
	)

	for _, test := range []struct {
		name              string
		challenge         *acmev1.Challenge
		service           *corev1.Service
		gateway           *networkingv1beta1.Gateway
		fallbackEnabled   bool
		expectIngress     bool
		expectVS          bool
		pass              bool
	}{
		{
			name:            "fallback disabled creates VirtualService",
			challenge:       getChallenge("test-challenge", "example", "test.example.com"),
			service:         getService("solver-svc", "example", "test.example.com", 8089),
			gateway:         getGateway("test-gateway", "example", nil),
			fallbackEnabled: false,
			expectIngress:   false,
			expectVS:        true,
			pass:            true,
		},
		{
			name:            "fallback enabled but annotation not present creates VirtualService",
			challenge:       getChallenge("test-challenge", "example", "test.example.com"),
			service:         getService("solver-svc", "example", "test.example.com", 8089),
			gateway:         getGateway("test-gateway", "example", nil),
			fallbackEnabled: true,
			expectIngress:   false,
			expectVS:        true,
			pass:            true,
		},
		{
			name:      "fallback enabled with dns-disabled annotation creates Ingress",
			challenge: getChallenge("test-challenge", "example", "test.example.com"),
			service:   getService("solver-svc", "example", "test.example.com", 8089),
			gateway: getGateway("test-gateway", "example", map[string]string{
				dnsDisabledAnnotation: "true",
			}),
			fallbackEnabled: true,
			expectIngress:   true,
			expectVS:        false,
			pass:            true,
		},
		{
			name:      "fallback enabled with dns-disabled=false creates VirtualService",
			challenge: getChallenge("test-challenge", "example", "test.example.com"),
			service:   getService("solver-svc", "example", "test.example.com", 8089),
			gateway: getGateway("test-gateway", "example", map[string]string{
				dnsDisabledAnnotation: "false",
			}),
			fallbackEnabled: true,
			expectIngress:   false,
			expectVS:        true,
			pass:            true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ics := istiofake.NewSimpleClientset()

			th := testHelper{
				ics: ics,
				ccs: certmanagerfake.NewSimpleClientset(),
				scs: &fakeServiceLister{Service: test.service},
				glc: cache.New(),
				kcs: k8sfake.NewSimpleClientset(),
			}

			th.ccs.AcmeV1().(*acmefake.FakeAcmeV1).PrependReactor(
				"get",
				"challenges",
				getChallengeFunc(test.challenge))

			if test.gateway != nil {
				th.glc.Add(fmt.Sprintf("%s/%s", test.gateway.Namespace, test.gateway.Name), test.challenge.Spec.DNSName)

				th.ics.NetworkingV1beta1().(*networkingv1beta1fake.FakeNetworkingV1beta1).PrependReactor(
					"get",
					"gateways",
					func(action k8stesting.Action) (bool, runtime.Object, error) {
						return true, test.gateway, nil
					})
			}

			vs := networkingv1beta1.VirtualService{}
			vs.Name = test.challenge.Name
			vs.Namespace = test.challenge.Namespace
			th.ics.NetworkingV1beta1().(*networkingv1beta1fake.FakeNetworkingV1beta1).PrependReactor(
				"patch",
				"virtualservices",
				func(action k8stesting.Action) (bool, runtime.Object, error) {
					return true, &vs, nil
				})

			opts := []challengesolver.OptionsFunc{}
			if test.fallbackEnabled {
				opts = append(opts, challengesolver.WithFallbackIngress(challengesolver.FallbackIngressConfig{
					Enabled:               true,
					DNSDisabledAnnotation: dnsDisabledAnnotation,
					IngressClass:          ingressClass,
				}))
			}

			cs := challengesolver.NewChallengeSolver(th.scs, th.ics, th.ccs, th.kcs, th.glc, opts...)

			out, err := cs.Solve(context.Background(), test.challenge)

			if test.pass {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}

			if test.expectVS {
				assert.NotNil(t, out, "expected VirtualService to be created")
			} else {
				assert.Nil(t, out, "expected no VirtualService")
			}

			if test.expectIngress {
				ingresses, err := th.kcs.NetworkingV1().Ingresses(test.challenge.Namespace).List(context.Background(), metav1.ListOptions{})
				assert.NoError(t, err)
				assert.Len(t, ingresses.Items, 1, "expected one Ingress to be created")

				if len(ingresses.Items) > 0 {
					ingress := ingresses.Items[0]
					assert.Equal(t, test.challenge.Name, ingress.Name)
					assert.Equal(t, ingressClass, *ingress.Spec.IngressClassName)
					assert.Len(t, ingress.Spec.Rules, 1)
					assert.Equal(t, test.challenge.Spec.DNSName, ingress.Spec.Rules[0].Host)
					assert.Contains(t, ingress.Spec.Rules[0].HTTP.Paths[0].Path, "/.well-known/acme-challenge/")
				}
			}
		})
	}
}

func TestIngressFromChallengeMeta(t *testing.T) {
	cm := challengesolver.ChallengeMeta{
		Port:      8089,
		Service:   "solver-svc",
		DNSName:   "test.example.com",
		Namespace: "test-ns",
		Token:     "test-token-123",
		Name:      "test-challenge",
		UID:       types.UID("uid-12345"),
		Gateway:   "test-ns/test-gateway",
	}

	ingress := challengesolver.IngressFromChallengeMeta(cm, "traefik")

	assert.Equal(t, "test-challenge", ingress.Name)
	assert.Equal(t, "test-ns", ingress.Namespace)
	assert.Equal(t, "traefik", *ingress.Spec.IngressClassName)

	assert.Len(t, ingress.OwnerReferences, 1)
	assert.Equal(t, "Challenge", ingress.OwnerReferences[0].Kind)
	assert.Equal(t, "test-challenge", ingress.OwnerReferences[0].Name)
	assert.Equal(t, types.UID("uid-12345"), ingress.OwnerReferences[0].UID)

	assert.Len(t, ingress.Spec.Rules, 1)
	assert.Equal(t, "test.example.com", ingress.Spec.Rules[0].Host)
	assert.Equal(t, "/.well-known/acme-challenge/test-token-123", ingress.Spec.Rules[0].HTTP.Paths[0].Path)
	assert.Equal(t, "solver-svc", ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name)
	assert.Equal(t, int32(8089), ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Number)
}

func getGateway(name, namespace string, annotations map[string]string) *networkingv1beta1.Gateway {
	return &networkingv1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: "networking.istio.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: istiov1beta1.Gateway{
			Servers: []*istiov1beta1.Server{
				{
					Hosts: []string{"test.example.com"},
					Port: &istiov1beta1.Port{
						Number:   443,
						Name:     "https",
						Protocol: "HTTPS",
					},
				},
			},
		},
	}
}
