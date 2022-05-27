package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	secretNameMaxLength        = 253
	credentialNameRandomStrLen = 10
)

type GatewayMutationHook struct {
	istioClient istioversionedclient.Interface
	decoder     *admission.Decoder
}

func NewGatewayMutationHook(client istioversionedclient.Interface) *GatewayMutationHook {
	gmh := &GatewayMutationHook{istioClient: client}
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

	mutate(gateway, log)

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

func credentialName(namespace, name string, log logr.Logger) string {
	prefix := fmt.Sprintf("%s-%s", namespace, name)
	randomStr := rand.String(credentialNameRandomStrLen)

	// Leave enough space for "-<random string>"
	maxPrefixLen := secretNameMaxLength - credentialNameRandomStrLen - 1

	credentialName := fmt.Sprintf("%s-%s", prefix, randomStr)

	if len(prefix) > maxPrefixLen {
		original := credentialName
		credentialName = fmt.Sprintf("%s-%s", prefix[:maxPrefixLen], randomStr)
		log.Info(fmt.Sprintf("truncating gateway %s credentialName %s to %s", name, original, credentialName))
	}

	return credentialName
}

func mutate(gateway *v1beta1.Gateway, log logr.Logger) {
	for _, s := range gateway.Spec.Servers {
		if s.Tls == nil {
			continue
		}

		if s.Tls.Mode == networkingv1beta1.ServerTLSSettings_SIMPLE {
			newCredentialName := credentialName(gateway.Namespace, gateway.Name, log)
			log.Info(fmt.Sprintf("mutating gateway %s Tls.CredentialName, %s to %s", gateway.Name, s.Tls.CredentialName, newCredentialName))

			s.Tls.CredentialName = newCredentialName
		}
	}
}
