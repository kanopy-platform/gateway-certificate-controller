package challengesolver_test

import (
	"context"
	"fmt"
	"hash/adler32"
	"testing"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/challengesolver"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stretchr/testify/assert"

	certmanagerfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	acmefake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1/fake"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	networkingv1beta1fake "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	corev1fake "k8s.io/client-go/kubernetes/typed/core/v1/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestStub(t *testing.T) {
	t.Parallel()

	glc := cache.New()

	assert.NotNil(t, glc)
}

type testHelper struct {
	ics *istiofake.Clientset
	ccs *certmanagerfake.Clientset
	scs *k8sfake.Clientset
	glc *cache.GatewayLookupCache
}

func (th *testHelper) newTestSolver() *challengesolver.ChallengeSolver {
	return challengesolver.NewChallengeSolver(th.scs.CoreV1(), th.ics.NetworkingV1beta1(), th.ccs.AcmeV1(), th.glc)
}

func TestSolver(t *testing.T) {
	/*
		th := testHelper{
			ics: istiofake.NewSimpleClientset(),
			ccs: certmanagerfake.NewSimpleClientset(),
			scs: k8sfake.NewSimpleClientset(),
			glc: cache.New(),
		}

		th.ccs.AcmeV1().(*acmefake.FakeAcmeV1).PrependReactor(
			"get",
			"challenges",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true,
					&acmev1.Challenge{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "testy",
							Namespace: "example",
							UID:       "12345",
						},
						Spec: acmev1.ChallengeSpec{
							Token:   "testtoken",
							DNSName: "thing.example.com",
						},
					}, nil
			})

		th.scs.CoreV1().(*corev1fake.FakeCoreV1).PrependReactor(
			"list",
			"services",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true,
					&corev1.ServiceList{
						Items: []corev1.Service{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "testservice",
									Namespace: "example",
									Labels: map[string]string{
										"acme.cert-manager.io/http-domain": fmt.Sprint(adler32.Checksum([]byte("thing.example.com"))),
										"acme.cert-manager.io/http-token":  fmt.Sprint(adler32.Checksum([]byte("testtoken"))),
									},
								},
								Spec: corev1.ServiceSpec{
									Ports: []corev1.ServicePort{
										{
											Port: int32(8089),
										},
									}},
							},
						},
					}, nil
			})
	*/
	for _, test := range []struct {
		name           string
		challenge      *acmev1.Challenge
		service        corev1.Service
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
			name:        "No Service",
			challenge:   getChallenge("noservice", "example", "noservice.com"),
			gatewayName: "gateway",
			pass:        false,
		},
		{
			name:        "No Service Port",
			challenge:   getChallenge("noservice", "example", "noservice.com"),
			gatewayName: "gateway",
			service:     getService("service", "example", "service.com", 0),
			pass:        false,
		},
		{
			name:        "Service",
			challenge:   getChallenge("noservice", "example", "service.com"),
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
			scs: k8sfake.NewSimpleClientset(),
			glc: cache.New(),
		}
		th.scs.CoreV1().(*corev1fake.FakeCoreV1).PrependReactor(
			"list",
			"services",
			listServiceFunc(test.service))
		th.ccs.AcmeV1().(*acmefake.FakeAcmeV1).PrependReactor(
			"get",
			"challenges",
			getChallengeFunc(test.challenge))

		if test.challenge != nil {

			th.glc.Add(fmt.Sprintf("%s/%s", test.challenge.Namespace, test.gatewayName), test.challenge.Spec.DNSName)

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
			}
		}

	} /*
		th.ics.NetworkingV1beta1().(*networkingv1beta1fake.FakeNetworkingV1beta1).PrependReactor(
			"patch",
			"virtualservices",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true,
					&vs,
					nil
			})
		cs := th.newTestSolver()

		th.glc.Add("example/testgateway", "thing.example.com")

		req := reconcile.Request{}
		req.Namespace = "example"
		req.Name = "testy"

		ctx := klog.NewContext(context.Background(), zap.New(zap.Level(zapcore.DebugLevel)))
		out, err := cs.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.NotNil(t, out)
	*/

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

func getChallengeFuncWithError(err error) k8stesting.ReactionFunc {
	var chal *acmev1.Challenge
	return getReactionFuncWithError(chal, err)
}

func getService(name, namespace, dnsName string, port int) corev1.Service {
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
	return svc
}

func listServiceFunc(services ...corev1.Service) k8stesting.ReactionFunc {
	return func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.ServiceList{Items: services}, nil
	}
}

func listServiceFuncWithError(err error) k8stesting.ReactionFunc {
	var slist *corev1.ServiceList
	return getReactionFuncWithError(slist, err)
}

func getReactionFuncWithError(obj runtime.Object, err error) k8stesting.ReactionFunc {
	return func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, obj, err
	}
}
