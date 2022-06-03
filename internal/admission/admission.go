package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

	gateway = mutate(ctx, gateway)

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

func credentialName(ctx context.Context, namespace, name string) string {
	log := log.FromContext(ctx)

	prefix := fmt.Sprintf("%s-%s", namespace, name)
	randomStr := rand.String(credentialNameRandomStrLen)

	// Leave enough space for a dash and the random string
	maxPrefixLen := secretNameMaxLength - credentialNameRandomStrLen - 1

	if len(prefix) > maxPrefixLen {
		prefix = prefix[:maxPrefixLen]
		log.Info(fmt.Sprintf("truncating gateway %s credentialName to %s", name, prefix))
	}

	return fmt.Sprintf("%s-%s", prefix, randomStr)
}

func getGatewayNameFromCredentialName(credentialName []string) string {
	const excludeNamespace = 1
	excludeRandomSuffix := len(credentialName) - 1
	return strings.Join(credentialName[excludeNamespace:excludeRandomSuffix], "-")
}

func canMutateCredentialName(current string, namespace string, gatewayName string) bool {
	parts := strings.Split(current, "-")
	if len(parts) >= 3 {
		pgn := getGatewayNameFromCredentialName(parts)
		if parts[0] == namespace && pgn == gatewayName {
			return false
		}
	}
	return true
}

func mutate(ctx context.Context, gateway *v1beta1.Gateway) *v1beta1.Gateway {
	log := log.FromContext(ctx)

	for _, s := range gateway.Spec.Servers {
		if s.Tls == nil {
			continue
		}

		if s.Tls.Mode == networkingv1beta1.ServerTLSSettings_SIMPLE {
			if canMutateCredentialName(s.Tls.CredentialName, gateway.Namespace, gateway.Name) {
				newCredentialName := credentialName(ctx, gateway.Namespace, gateway.Name)
				log.Info(fmt.Sprintf("mutating gateway %s Tls.CredentialName, %s to %s", gateway.Name, s.Tls.CredentialName, newCredentialName))
				s.Tls.CredentialName = newCredentialName
			}
		}
	}
	return gateway
}