package v1beta1

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	certmanagerfake "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/fake"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	networkingv1beta1fake "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1certmanager "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	v1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

const TestNamespace = "test"
const TestGatewayName = "mygateway"
const TestCertificateName = "mygateway-cert"
const TestCertNamespace = "certnamespace"

type controllerSpy struct {
	*GatewayController
	CleanupCalled int
	CreateCalled  int
	UpdateCalled  int
	Error         bool
}

type GatewayOptions struct {
	Certificates   []runtime.Object
	Hosts          []string
	CredentialName string
	Annotations    map[string]string
	Labels         map[string]string
	Servers        []*networkingv1beta1.Server
}

func WithCredentialName(name string) func(*GatewayOptions) {
	return func(gopt *GatewayOptions) {
		gopt.CredentialName = name
	}
}

func WithAnnotations(annotations map[string]string) func(*GatewayOptions) {
	return func(gopt *GatewayOptions) {
		gopt.Annotations = annotations
	}
}

func WithLabels(labels map[string]string) func(*GatewayOptions) {
	return func(gopt *GatewayOptions) {
		gopt.Labels = labels
	}
}

func AppendServer(server *networkingv1beta1.Server) func(*GatewayOptions) {
	return func(gopt *GatewayOptions) {
		gopt.Servers = append(gopt.Servers, server)
	}
}

func AppendHosts(host string) func(*GatewayOptions) {
	return func(gopt *GatewayOptions) {
		gopt.Hosts = append(gopt.Hosts, host)
	}
}

func WithHosts(hosts ...string) func(*GatewayOptions) {
	return func(gopt *GatewayOptions) {
		gopt.Hosts = hosts
	}
}

func AppendCertificates(o ...runtime.Object) func(*GatewayOptions) {
	return func(gopt *GatewayOptions) {
		gopt.Certificates = append(gopt.Certificates, o...)
	}
}

func NewGatewayOptions(opts ...func(*GatewayOptions)) *GatewayOptions {
	gopts := &GatewayOptions{
		Hosts:          []string{"test2.example.com", "test1.example.com"},
		CredentialName: TestCertificateName,
	}

	for _, o := range opts {
		o(gopts)
	}

	return gopts
}

func gatewayListAction(opts ...func(*GatewayOptions)) func(k8stesting.Action) (bool, runtime.Object, error) {
	gopts := NewGatewayOptions(opts...)
	return func(action k8stesting.Action) (bool, runtime.Object, error) {

		servers := []*networkingv1beta1.Server{
			{
				Hosts: gopts.Hosts,
				Tls: &networkingv1beta1.ServerTLSSettings{
					CredentialName: gopts.CredentialName,
					Mode:           networkingv1beta1.ServerTLSSettings_SIMPLE,
				},
			},
		}

		servers = append(servers, gopts.Servers...)

		return true, &v1beta1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: "networking.istio.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        TestGatewayName,
				Namespace:   TestNamespace,
				Annotations: gopts.Annotations,
				Labels:      gopts.Labels,
			},
			Spec: networkingv1beta1.Gateway{
				Servers: servers,
			},
		}, nil
	}
}

type TestHelper struct {
	IstioClient *istiofake.Clientset
	CertClient  *certmanagerfake.Clientset
	Controller  *controllerSpy
}

func NewTestHelper(certificates ...runtime.Object) *TestHelper {
	ics := istiofake.NewSimpleClientset()
	ccs := certmanagerfake.NewSimpleClientset(certificates...)
	return &TestHelper{
		IstioClient: ics,
		CertClient:  ccs,
		Controller:  setupControllerWithSpy(ics, ccs),
	}
}

func NewTestHelperWithGateways(opts ...func(*GatewayOptions)) *TestHelper {
	gopts := NewGatewayOptions(opts...)
	helper := NewTestHelper(gopts.Certificates...)

	helper.IstioClient.NetworkingV1beta1().(*networkingv1beta1fake.FakeNetworkingV1beta1).PrependReactor(
		"get",
		"gateways",
		gatewayListAction(opts...),
	)

	return helper
}

func NewTestHelperWithCertificates(opts ...func(*GatewayOptions)) *TestHelper {
	opts = append(opts, AppendCertificates(&v1certmanager.Certificate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Certificate",
			APIVersion: "cert-manager.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestCertificateName,
			Namespace: TestCertNamespace,
		},
		Spec: v1certmanager.CertificateSpec{
			DNSNames: []string{"test1.example.com", "test2.example.com"},
			IssuerRef: v1.ObjectReference{
				Kind:  "ClusterIssuer",
				Name:  "default",
				Group: "cert-manager.io",
			},
		},
	}))

	helper := NewTestHelperWithGateways(opts...)
	return helper
}

func setupControllerWithSpy(cs *istiofake.Clientset, certFake *certmanagerfake.Clientset) *controllerSpy {
	spy := &controllerSpy{
		GatewayController: NewGatewayController(cs, certFake, TestCertNamespace),
	}
	spy.GatewayController.events = spy
	return spy
}

func (r *controllerSpy) Cleanup(ctx context.Context, request reconcile.Request) error {
	r.CleanupCalled++
	if r.Error {
		return fmt.Errorf("mock error")
	}
	return r.GatewayController.Cleanup(ctx, request)
}

func (r *controllerSpy) CreateCertificate(ctx context.Context, gateway *v1beta1.Gateway, server *networkingv1beta1.Server) error {
	r.CreateCalled++
	if r.Error {
		return fmt.Errorf("mock create error")
	}
	return r.GatewayController.CreateCertificate(ctx, gateway, server)
}

func (r *controllerSpy) UpdateCertificate(ctx context.Context, cert *v1certmanager.Certificate, gateway *v1beta1.Gateway, server *networkingv1beta1.Server) error {
	r.UpdateCalled++
	if r.Error {
		return fmt.Errorf("mock update error")
	}
	return r.GatewayController.UpdateCertificate(ctx, cert, gateway, server)
}

func reconcileRequest() reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: TestNamespace,
			Name:      TestGatewayName,
		},
	}
}

func assertCreateCertificateCalled(t *testing.T, helper *TestHelper) {
	r, err := helper.Controller.Reconcile(context.TODO(), reconcileRequest())
	assert.NoError(t, err)
	assert.Equal(t, 1, helper.Controller.CreateCalled)
	assert.Equal(t, reconcile.Result{}, r)
}

func assertCertificateUpdated(t *testing.T, helper *TestHelper) {
	_, err := helper.Controller.Reconcile(context.TODO(), reconcileRequest())
	assert.NoError(t, err)
	assert.Equal(t, 0, helper.Controller.CreateCalled)
	assert.Equal(t, 1, helper.Controller.UpdateCalled)
}

func TestGatewayControllerReconcileNoError(t *testing.T) {
	t.Parallel()
	g := NewGatewayController(istiofake.NewSimpleClientset(), certmanagerfake.NewSimpleClientset(), "default")
	r, err := g.Reconcile(context.TODO(), reconcile.Request{})
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, r)
}

func TestGatewayReconcile_CallsCleanupOnNotExists(t *testing.T) {
	t.Parallel()
	helper := NewTestHelper()
	r, err := helper.Controller.Reconcile(context.TODO(), reconcileRequest())
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, r)
	assert.Equal(t, 1, helper.Controller.CleanupCalled)
}

func TestGatewayReconcile_CleanupErrors(t *testing.T) {
	t.Parallel()
	helper := NewTestHelper()
	helper.Controller.Error = true
	r, err := helper.Controller.Reconcile(context.TODO(), reconcileRequest())
	assert.Error(t, err)
	assert.Equal(t, reconcile.Result{Requeue: true}, r)
}

func TestGatewayReconcile_CallsCreateCertificate(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways()
	assertCreateCertificateCalled(t, helper)
}

func TestGatewayReconcile_CallsCreateCertificateWithError(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways()
	helper.Controller.Error = true
	r, err := helper.Controller.Reconcile(context.TODO(), reconcileRequest())
	assert.Error(t, err)
	assert.Equal(t, reconcile.Result{Requeue: true}, r)
}

func TestGatewyReconcile_CreatesCertificateForGateway(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways()
	assertCreateCertificateCalled(t, helper)
	certList, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).List(context.TODO(), metav1.ListOptions{})
	assert.NoError(t, err)
	assert.Len(t, certList.Items, 1)
}

func TestGatewayReconcile_CreateCertificateUsingCredentialName(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways()
	assertCreateCertificateCalled(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.NotNil(t, cert)
}

func TestGatewayReconcile_CreateCertificateLabeledAsManaged(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways()
	assertCreateCertificateCalled(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	assert.Equal(t, "true", cert.Labels["v1beta1.kanopy-platform.github.io/istio-cert-controller-managed"])
}

func TestGatewayReconcile_CreateCertificateAnnotatedWithGatewayName(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways()
	assertCreateCertificateCalled(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	assert.Equal(t, fmt.Sprintf("%s.%s", TestGatewayName, TestNamespace), cert.Annotations["v1beta1.kanopy-platform.github.io/istio-cert-controller-managed"])
}

func TestGatewayReconcile_CreateCertificateWithHosts(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways()
	assertCreateCertificateCalled(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, []string{"test1.example.com", "test2.example.com"}, cert.Spec.DNSNames)
}

func TestGatewayReconcile_CreatCertificateWithDefaultIssuer(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways()
	assertCreateCertificateCalled(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "default", cert.Spec.IssuerRef.Name)
	assert.Equal(t, "ClusterIssuer", cert.Spec.IssuerRef.Kind)
}

func TestGatewayReconcile_CreatCertificateWithClusterIssuerFromGatewayAnnotation(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways(WithAnnotations(map[string]string{
		"v1beta1.kanopy-platform.github.io/istio-cert-controller-issuer": "testissuer",
	}))
	assertCreateCertificateCalled(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "testissuer", cert.Spec.IssuerRef.Name)
	assert.Equal(t, "ClusterIssuer", cert.Spec.IssuerRef.Kind)
}

func TestGatewayReconcile_SkipCertificateForTLSModeNotSimple(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithGateways(AppendServer(&networkingv1beta1.Server{
		Hosts: []string{"pass.example.com"},
		Tls: &networkingv1beta1.ServerTLSSettings{
			Mode: networkingv1beta1.ServerTLSSettings_AUTO_PASSTHROUGH,
		},
	}))
	_, err := helper.Controller.Reconcile(context.TODO(), reconcileRequest())
	assert.NoError(t, err)
	certList, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).List(context.TODO(), metav1.ListOptions{})
	assert.NoError(t, err)
	assert.Len(t, certList.Items, 1)
}

func TestGatewayReconcile_CallsUpdateCertificateWhenCertificateExists(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithCertificates()
	assertCertificateUpdated(t, helper)
}

func TestGatewayReconcile_UpdatesCertificateWithNewHost(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithCertificates(AppendHosts("an.example.com"))
	assertCertificateUpdated(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, []string{"an.example.com", "test1.example.com", "test2.example.com"}, cert.Spec.DNSNames)
}

func TestGatewayReconcile_UpdatesCertificateWithDeletedHost(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithCertificates(WithHosts("test2.example.com"))
	assertCertificateUpdated(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, []string{"test2.example.com"}, cert.Spec.DNSNames)
}

func TestGatewayReconcile_UpdatesCertificateWithNewIssue(t *testing.T) {
	t.Parallel()
	helper := NewTestHelperWithCertificates(WithAnnotations(map[string]string{
		"v1beta1.kanopy-platform.github.io/istio-cert-controller-issuer": "new",
	}))
	assertCertificateUpdated(t, helper)
	cert, err := helper.CertClient.CertmanagerV1().Certificates(TestCertNamespace).Get(context.TODO(), TestCertificateName, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "new", cert.Spec.IssuerRef.Name)
	assert.Equal(t, "ClusterIssuer", cert.Spec.IssuerRef.Kind)
}
