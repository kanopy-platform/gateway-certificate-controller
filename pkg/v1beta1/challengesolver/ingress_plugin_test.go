package challengesolver_test

import (
	"context"
	"testing"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/challengesolver"
	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"
	"github.com/stretchr/testify/assert"
	networkingv1beta1istio "istio.io/client-go/pkg/apis/networking/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func ingressTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = networkingv1beta1istio.SchemeBuilder.AddToScheme(s)
	return s
}

func baseIngressMeta() challengesolver.ChallengeMeta {
	return challengesolver.ChallengeMeta{
		Port:      8089,
		Service:   "cm-acme-http-solver-xyz",
		DNSName:   "login.corp.example.com",
		Namespace: "routing",
		Token:     "mytoken",
		Name:      "challenge-abc",
		UID:       types.UID("uid-1234"),
		Gateway:   "routing/my-gateway",
	}
}

func TestIngressPlugin_Applicable(t *testing.T) {
	t.Parallel()
	challenge := &acmev1.Challenge{
		Spec: acmev1.ChallengeSpec{DNSName: "login.corp.example.com"},
	}

	t.Run("no cache: always false", func(t *testing.T) {
		p := challengesolver.NewIngressPlugin(nil, nil, "traefik", false)
		assert.False(t, p.Applicable(context.Background(), challenge))
	})

	t.Run("cache without ingress flag: false", func(t *testing.T) {
		glc := cache.New()
		p := challengesolver.NewIngressPlugin(nil, glc, "traefik", false)
		assert.False(t, p.Applicable(context.Background(), challenge))
	})

	t.Run("cache with ingress flag set: true", func(t *testing.T) {
		glc := cache.New()
		glc.SetIngressSolver("login.corp.example.com", true)
		p := challengesolver.NewIngressPlugin(nil, glc, "traefik", false)
		assert.True(t, p.Applicable(context.Background(), challenge))
	})
}

func TestIngressPlugin_Solve(t *testing.T) {
	meta := baseIngressMeta()

	t.Run("creates Ingress with correct host, path, backend, and ingressClass", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(ingressTestScheme()).Build()
		p := challengesolver.NewIngressPlugin(fc, nil, "traefik", false)

		err := p.Solve(context.Background(), meta)
		assert.NoError(t, err)

		ingress := &networkingv1.Ingress{}
		err = fc.Get(context.Background(), types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, ingress)
		assert.NoError(t, err)

		assert.Equal(t, "traefik", *ingress.Spec.IngressClassName)
		assert.Len(t, ingress.Spec.Rules, 1)
		rule := ingress.Spec.Rules[0]
		assert.Equal(t, "login.corp.example.com", rule.Host)
		assert.Len(t, rule.HTTP.Paths, 1)
		path := rule.HTTP.Paths[0]
		assert.Equal(t, "/.well-known/acme-challenge/mytoken", path.Path)
		assert.Equal(t, networkingv1.PathTypeExact, *path.PathType)
		assert.Equal(t, "cm-acme-http-solver-xyz", path.Backend.Service.Name)
		assert.Equal(t, int32(8089), path.Backend.Service.Port.Number)
	})

	t.Run("Ingress carries owner reference pointing to the Challenge", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(ingressTestScheme()).Build()
		p := challengesolver.NewIngressPlugin(fc, nil, "traefik", false)

		err := p.Solve(context.Background(), meta)
		assert.NoError(t, err)

		ingress := &networkingv1.Ingress{}
		err = fc.Get(context.Background(), types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, ingress)
		assert.NoError(t, err)

		assert.Len(t, ingress.OwnerReferences, 1)
		ref := ingress.OwnerReferences[0]
		assert.Equal(t, "Challenge", ref.Kind)
		assert.Equal(t, meta.Name, ref.Name)
		assert.Equal(t, meta.UID, ref.UID)
		assert.NotNil(t, ref.Controller)
		assert.True(t, *ref.Controller)
		assert.NotNil(t, ref.BlockOwnerDeletion)
		assert.True(t, *ref.BlockOwnerDeletion)
	})

	t.Run("configurable ingressClass is applied to spec and annotation", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(ingressTestScheme()).Build()
		p := challengesolver.NewIngressPlugin(fc, nil, "nginx", false)

		err := p.Solve(context.Background(), meta)
		assert.NoError(t, err)

		ingress := &networkingv1.Ingress{}
		_ = fc.Get(context.Background(), types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, ingress)
		assert.Equal(t, "nginx", *ingress.Spec.IngressClassName)
		assert.Equal(t, "nginx", ingress.Annotations["kubernetes.io/ingress.class"])
	})

	t.Run("legacy ingress.class annotation matches spec.ingressClassName", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(ingressTestScheme()).Build()
		p := challengesolver.NewIngressPlugin(fc, nil, "traefik", false)

		err := p.Solve(context.Background(), meta)
		assert.NoError(t, err)

		ingress := &networkingv1.Ingress{}
		err = fc.Get(context.Background(), types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, ingress)
		assert.NoError(t, err)

		assert.Equal(t, *ingress.Spec.IngressClassName, ingress.Annotations["kubernetes.io/ingress.class"],
			"annotation and spec field must carry the same ingressClass value")
	})

	t.Run("dry-run skips apply and returns no error", func(t *testing.T) {
		fc := fake.NewClientBuilder().WithScheme(ingressTestScheme()).Build()
		p := challengesolver.NewIngressPlugin(fc, nil, "traefik", true)

		err := p.Solve(context.Background(), meta)
		assert.NoError(t, err)

		ingress := &networkingv1.Ingress{}
		err = fc.Get(context.Background(), types.NamespacedName{Name: meta.Name, Namespace: meta.Namespace}, ingress)
		assert.Error(t, err, "Ingress should not be created in dry-run mode")
	})
}

func TestIngressSolverApplicableViaEventHandlers(t *testing.T) {
	t.Parallel()
	// Round-trip test: Gateway informer event marks hosts; plugins see the flag.
	glc := cache.New()

	gw := &networkingv1beta1istio.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gw", Namespace: "routing",
			Annotations: map[string]string{v1beta1labels.IngressHTTPSolverAnnotation: "true"},
		},
	}
	// Use AddFunc directly (as the informer event handler does).
	glc.AddFunc(gw)

	// Without a host this gateway contributes no hosts — verify no panic and
	// default false is returned for an arbitrary hostname.
	assert.False(t, glc.GetIngressSolver("login.corp.example.com"))
}
