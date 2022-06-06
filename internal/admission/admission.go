package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/version"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	secretNameMaxLength        = 253
	credentialNameRandomStrLen = 10
)

var (
	lastAppliedMutationAnnotation = fmt.Sprintf("%s/last-applied-mutation", version.String())
)

type GatewayMutationHook struct {
	istioClient istioversionedclient.Interface
	decoder     *admission.Decoder
}

func NewGatewayMutationHook(client istioversionedclient.Interface) *GatewayMutationHook {
	gmh := &GatewayMutationHook{
		istioClient: client,
	}
	return gmh
}

func (g *GatewayMutationHook) SetupWithManager(mgr manager.Manager) {
	mgr.GetWebhookServer().Register("/mutate", &webhook.Admission{Handler: g})
}

func (g *GatewayMutationHook) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := log.FromContext(ctx)

	gateway := &v1beta1.Gateway{}

	err := g.decoder.Decode(req, gateway)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to decode gateway request: %s", req.Name))
		return admission.Errored(http.StatusBadRequest, err)
	}

	gateway, err = mutate(ctx, gateway.DeepCopy())
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to mutate gateway: %s", gateway.Name))
		return admission.Errored(http.StatusInternalServerError, err)
	}

	jsonGateway, err := json.Marshal(gateway)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to marshal gateway: %s", gateway.Name))
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, jsonGateway)
}

func (g *GatewayMutationHook) InjectDecoder(d *admission.Decoder) error {
	g.decoder = d
	return nil
}

func credentialName(ctx context.Context, namespace, name string, portName string) string {
	log := log.FromContext(ctx)
	prefix := fmt.Sprintf("%s-%s", namespace, name)
	// Leave enough space for a dash and the random string
	maxPrefixLen := secretNameMaxLength - len(portName) - 1

	if len(prefix) > maxPrefixLen {
		prefix = prefix[:maxPrefixLen]
		log.Info(fmt.Sprintf("truncating gateway %s credentialName to %s", name, prefix))
	}

	return fmt.Sprintf("%s-%s", prefix, portName)
}

func mutate(ctx context.Context, gateway *v1beta1.Gateway) (*v1beta1.Gateway, error) {
	log := log.FromContext(ctx)

	for _, s := range gateway.Spec.Servers {
		if s.Tls == nil {
			continue
		}

		if s.Tls.Mode == networkingv1beta1.ServerTLSSettings_SIMPLE {
			newCredentialName := credentialName(ctx, gateway.Namespace, gateway.Name, s.Port.Name)
			log.Info(fmt.Sprintf("mutating gateway %s Tls.CredentialName, %s to %s", gateway.Name, s.Tls.CredentialName, newCredentialName))
			s.Tls.CredentialName = newCredentialName
		}
	}

	return gateway, nil
}
