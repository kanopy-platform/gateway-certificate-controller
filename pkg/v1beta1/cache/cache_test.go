package cache_test

import (
	"fmt"
	"testing"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"
	"github.com/stretchr/testify/assert"

	networkingv1beta1 "istio.io/api/networking/v1beta1"
	v1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGatewayLookupCache(t *testing.T) {
	t.Parallel()

	glc := cache.New()

	tests := []struct {
		gw         string
		hosts      []string
		del        bool
		getInitial bool
		getAfter   bool
		name       string
	}{
		{
			gw: "testNS/testGW",
			hosts: []string{
				"a.example.com",
				"b.example.com",
			},
			getAfter: true,
			name:     "setup",
		},
		{
			gw: "testNS/replacementGateway",
			hosts: []string{
				"a.example.com",
				"b.example.com",
			},
			getInitial: true,
			getAfter:   true,
			name:       "update",
		},
		{
			gw: "testNS/replacementGateway",
			hosts: []string{
				"a.example.com",
				"b.example.com",
			},
			del:        true,
			name:       "delete",
			getInitial: true,
		},
		{
			name: "deleted",
		},
	}

	for _, test := range tests {
		_, ok := glc.Get("a.example.com")
		assert.Equal(t, test.getInitial, ok, test.name+" before")
		glc.Add(test.gw, test.hosts...)
		if test.del {
			glc.Delete(test.hosts...)
		}

		out, ok := glc.Get("a.example.com")

		assert.Equal(t, test.getAfter, ok, test.name+" after")
		if ok {
			assert.Equal(t, test.gw, out, test.name)
		}

	}
}

func TestGatewayLookupCacheEventAddFunc(t *testing.T) {

	gw := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testy",
			Namespace: "example",
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				&networkingv1beta1.Server{
					Hosts: []string{
						"a.b.c.d",
						"a.example.com",
						"example/dns.host.name",
						"*.dns.example.com",
					},
				},
			},
		},
	}

	glc := cache.New()
	glc.AddFunc(gw)

	out, _ := glc.Get("a.b.c.d")
	assert.Equal(t, "example/testy", out)
	out, _ = glc.Get("a.example.com")
	assert.Equal(t, "example/testy", out)
	out, _ = glc.Get("dns.host.name")
	assert.Equal(t, "example/testy", out)

	_, ok := glc.Get("missing")
	assert.False(t, ok)
	_, ok = glc.Get("*.dns.example.com")
	assert.False(t, ok)

	gw = &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "missing",
			Namespace: "example",
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				&networkingv1beta1.Server{
					Hosts: []string{
						"missing",
					},
				},
			},
		},
	}

	glc.AddFunc(gw)
	out, ok = glc.Get("missing")
	assert.True(t, ok)
	assert.Equal(t, "example/missing", out)
}

func TestGatewayLookupCacheEventUpdateFunc(t *testing.T) {
	original := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testy",
			Namespace: "example",
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				&networkingv1beta1.Server{
					Hosts: []string{
						"a.b.c.d",
						"a.example.com",
					},
				},
			},
		},
	}

	updated := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testy",
			Namespace: "example",
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				&networkingv1beta1.Server{
					Hosts: []string{
						"a.b.c.d",
						"b.example.com",
						"x.y.z",
					},
				},
			},
		},
	}

	glc := cache.New()
	glc.AddFunc(original)

	_, ok := glc.Get("a.example.com")
	assert.True(t, ok)

	glc.UpdateFunc(original, updated)
	_, ok = glc.Get("a.example.com")
	assert.False(t, ok)
	for _, host := range updated.Spec.Servers[0].Hosts {

		out, ok := glc.Get(host)
		assert.True(t, ok)
		assert.Equal(t, fmt.Sprintf("%s/%s", updated.Namespace, updated.Name), out)
	}

}
func TestGatewayLookupCacheEventDeleteFunc(t *testing.T) {
	gw := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testy",
			Namespace: "example",
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				&networkingv1beta1.Server{
					Hosts: []string{
						"a.b.c.d",
						"a.example.com",
					},
				},
			},
		},
	}

	glc := cache.New()

	glc.AddFunc(gw)
	_, ok := glc.Get("a.example.com")
	assert.True(t, ok)

	glc.DeleteFunc(gw)
	for _, host := range gw.Spec.Servers[0].Hosts {

		_, ok := glc.Get(host)
		assert.False(t, ok)
	}

}

func TestGatewayLookupCacheEventFuncEdgeCases(t *testing.T) {

	thing := "notagatewaypointer"
	gw := &v1beta1.Gateway{}
	glc := cache.New()
	// Ensure that an error is returned if the input isn't a *Gateway
	assert.NotPanics(t, func() { glc.AddFunc(thing) })
	assert.NotPanics(t, func() { glc.UpdateFunc(thing, gw) })
	assert.NotPanics(t, func() { glc.UpdateFunc(gw, thing) })
	assert.NotPanics(t, func() { glc.DeleteFunc(thing) })

	// Ensure no error is returned for a nil gateway, a nil gateway results in no changes
	var ngw *v1beta1.Gateway
	assert.NotPanics(t, func() { glc.AddFunc(ngw) })
	assert.NotPanics(t, func() { glc.UpdateFunc(gw, ngw) })
	assert.NotPanics(t, func() { glc.UpdateFunc(ngw, gw) })
	assert.NotPanics(t, func() { glc.DeleteFunc(ngw) })
}

func TestIngressSolverDirectMethods(t *testing.T) {
	t.Parallel()
	glc := cache.New()

	assert.False(t, glc.GetIngressSolver("a.example.com"), "unknown host should return false")

	glc.SetIngressSolver("a.example.com", true)
	assert.True(t, glc.GetIngressSolver("a.example.com"))

	// Idempotent set-true
	glc.SetIngressSolver("a.example.com", true)
	assert.True(t, glc.GetIngressSolver("a.example.com"))

	// Unset
	glc.SetIngressSolver("a.example.com", false)
	assert.False(t, glc.GetIngressSolver("a.example.com"))

	// Multiple independent hosts
	glc.SetIngressSolver("b.example.com", true)
	glc.SetIngressSolver("c.example.com", false)
	assert.True(t, glc.GetIngressSolver("b.example.com"))
	assert.False(t, glc.GetIngressSolver("c.example.com"))
}

func gatewayWithAnnotation(name, namespace, host, annotation string) *v1beta1.Gateway {
	gw := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1beta1.Gateway{
			Servers: []*networkingv1beta1.Server{
				{Hosts: []string{host}},
			},
		},
	}
	if annotation != "" {
		gw.Annotations = map[string]string{
			v1beta1labels.IngressHTTPSolverAnnotation: annotation,
		}
	}
	return gw
}

func TestIngressSolverAddFunc(t *testing.T) {
	t.Parallel()

	t.Run("annotation absent: host not marked as ingress solver", func(t *testing.T) {
		glc := cache.New()
		gw := gatewayWithAnnotation("gw", "ns", "login.corp.example.com", "")
		glc.AddFunc(gw)
		assert.False(t, glc.GetIngressSolver("login.corp.example.com"))
	})

	t.Run("annotation true: host marked as ingress solver", func(t *testing.T) {
		glc := cache.New()
		gw := gatewayWithAnnotation("gw", "ns", "login.corp.example.com", "true")
		glc.AddFunc(gw)
		assert.True(t, glc.GetIngressSolver("login.corp.example.com"))
		// gateway is still in the host lookup cache
		_, ok := glc.Get("login.corp.example.com")
		assert.True(t, ok)
	})
}

func TestIngressSolverUpdateFunc(t *testing.T) {
	t.Parallel()

	t.Run("annotation added on update: host becomes ingress solver", func(t *testing.T) {
		glc := cache.New()
		original := gatewayWithAnnotation("gw", "ns", "login.corp.example.com", "")
		updated := gatewayWithAnnotation("gw", "ns", "login.corp.example.com", "true")
		glc.AddFunc(original)
		assert.False(t, glc.GetIngressSolver("login.corp.example.com"))
		glc.UpdateFunc(original, updated)
		assert.True(t, glc.GetIngressSolver("login.corp.example.com"))
	})

	t.Run("annotation removed on update: host no longer ingress solver", func(t *testing.T) {
		glc := cache.New()
		original := gatewayWithAnnotation("gw", "ns", "login.corp.example.com", "true")
		updated := gatewayWithAnnotation("gw", "ns", "login.corp.example.com", "")
		glc.AddFunc(original)
		assert.True(t, glc.GetIngressSolver("login.corp.example.com"))
		glc.UpdateFunc(original, updated)
		assert.False(t, glc.GetIngressSolver("login.corp.example.com"))
	})

	t.Run("removed host cleared from ingress solver map", func(t *testing.T) {
		glc := cache.New()
		original := &v1beta1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gw", Namespace: "ns",
				Annotations: map[string]string{v1beta1labels.IngressHTTPSolverAnnotation: "true"},
			},
			Spec: networkingv1beta1.Gateway{
				Servers: []*networkingv1beta1.Server{
					{Hosts: []string{"a.corp.example.com", "b.corp.example.com"}},
				},
			},
		}
		updated := &v1beta1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gw", Namespace: "ns",
				Annotations: map[string]string{v1beta1labels.IngressHTTPSolverAnnotation: "true"},
			},
			Spec: networkingv1beta1.Gateway{
				Servers: []*networkingv1beta1.Server{
					{Hosts: []string{"a.corp.example.com"}},
				},
			},
		}
		glc.AddFunc(original)
		assert.True(t, glc.GetIngressSolver("b.corp.example.com"))
		glc.UpdateFunc(original, updated)
		// b.corp.example.com was removed from the gateway spec
		assert.False(t, glc.GetIngressSolver("b.corp.example.com"))
		assert.True(t, glc.GetIngressSolver("a.corp.example.com"))
	})
}

func TestIngressSolverDeleteFunc(t *testing.T) {
	t.Parallel()

	glc := cache.New()
	gw := gatewayWithAnnotation("gw", "ns", "login.corp.example.com", "true")
	glc.AddFunc(gw)
	assert.True(t, glc.GetIngressSolver("login.corp.example.com"))

	glc.DeleteFunc(gw)
	assert.False(t, glc.GetIngressSolver("login.corp.example.com"))
	_, ok := glc.Get("login.corp.example.com")
	assert.False(t, ok)
}
