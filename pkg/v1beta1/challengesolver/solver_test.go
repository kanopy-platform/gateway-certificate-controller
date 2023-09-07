package challengesolver_test

import (
	"context"
	"fmt"
	"hash/adler32"
	"testing"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/challengesolver"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stretchr/testify/assert"

	certmanagerfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	acmefake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/acme/v1/fake"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	corev1fake "k8s.io/client-go/kubernetes/typed/core/v1/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/klog/v2"
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

func TestOne(t *testing.T) {
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

	cs := th.newTestSolver()

	th.glc.Add("example/testgateway", "thing.example.com")

	req := reconcile.Request{}
	req.Namespace = "example"
	req.Name = "testy"

	ctx := klog.NewContext(context.Background(), zap.New(zap.Level(zapcore.DebugLevel)))
	out, err := cs.Reconcile(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, out)

}
