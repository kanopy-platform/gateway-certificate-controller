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
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	networkingv1beta1fake "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"
)

type testHelper struct {
	ics *istiofake.Clientset
	ccs *certmanagerfake.Clientset
	scs *fakeServiceLister
	glc *cache.GatewayLookupCache
}

func (th *testHelper) newTestSolver() *challengesolver.ChallengeSolver {
	return challengesolver.NewChallengeSolver(th.scs, th.ics.NetworkingV1beta1(), th.ccs, th.glc)
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
