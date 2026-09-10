package challengesolver_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/challengesolver"
	"github.com/stretchr/testify/assert"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	networkingv1beta1fake "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1beta1/fake"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"
)

func TestVirtualServicePlugin_Applicable(t *testing.T) {
	p := challengesolver.NewVirtualServicePlugin(nil, false)
	assert.True(t, p.Applicable(context.Background(), nil),
		"VirtualServicePlugin should be applicable by default")
}

func TestVirtualServicePlugin_Solve(t *testing.T) {
	const (
		namespace = "routing"
		name      = "challenge-abc"
		dnsName   = "login.corp.example.com"
		token     = "mytoken"
		service   = "cm-acme-http-solver-xyz"
		gateway   = "routing/my-gateway"
	)

	uid := types.UID("uid-1234")

	meta := challengesolver.ChallengeMeta{
		Port:      8089,
		Service:   service,
		DNSName:   dnsName,
		Namespace: namespace,
		Token:     token,
		Name:      name,
		UID:       uid,
		Gateway:   gateway,
	}

	t.Run("applies VirtualService with correct shape", func(t *testing.T) {
		ics := istiofake.NewSimpleClientset()
		returnedVS := &networkingv1beta1.VirtualService{}
		returnedVS.Name = name
		returnedVS.Namespace = namespace

		ics.NetworkingV1beta1().(*networkingv1beta1fake.FakeNetworkingV1beta1).PrependReactor(
			"patch", "virtualservices",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, returnedVS, nil
			},
		)

		p := challengesolver.NewVirtualServicePlugin(ics.NetworkingV1beta1(), false)
		err := p.Solve(context.Background(), meta)
		assert.NoError(t, err)

		actions := ics.Actions()
		assert.Len(t, actions, 1, "expected one patch action on VirtualServices")
		assert.Equal(t, "patch", actions[0].GetVerb())
		assert.Equal(t, "virtualservices", actions[0].GetResource().Resource)
		assert.Equal(t, namespace, actions[0].GetNamespace())
	})

	t.Run("dry-run skips apply and returns no error", func(t *testing.T) {
		ics := istiofake.NewSimpleClientset()
		p := challengesolver.NewVirtualServicePlugin(ics.NetworkingV1beta1(), true)
		err := p.Solve(context.Background(), meta)
		assert.NoError(t, err)
		assert.Empty(t, ics.Actions(), "dry-run should not call the API")
	})

	t.Run("VirtualService carries owner reference pointing to the Challenge", func(t *testing.T) {
		ics := istiofake.NewSimpleClientset()
		var capturedAction k8stesting.Action
		ics.NetworkingV1beta1().(*networkingv1beta1fake.FakeNetworkingV1beta1).PrependReactor(
			"patch", "virtualservices",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				capturedAction = action
				return true, &networkingv1beta1.VirtualService{}, nil
			},
		)

		p := challengesolver.NewVirtualServicePlugin(ics.NetworkingV1beta1(), false)
		err := p.Solve(context.Background(), meta)
		assert.NoError(t, err)
		assert.NotNil(t, capturedAction)

		patchAction, ok := capturedAction.(k8stesting.PatchAction)
		assert.True(t, ok)
		patchBytes := patchAction.GetPatch()
		assert.Contains(t, string(patchBytes), fmt.Sprintf(`"name":"%s"`, name))
		assert.Contains(t, string(patchBytes), `"kind":"Challenge"`)
	})
}
