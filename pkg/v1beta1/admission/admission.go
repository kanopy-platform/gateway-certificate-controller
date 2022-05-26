package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/random"
	log "github.com/sirupsen/logrus"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
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
	gateway := &v1beta1.Gateway{}

	err := g.decoder.Decode(req, gateway)
	if err != nil {
		log.WithFields(log.Fields{
			"name": req.Name,
			"err":  err,
		}).Error("failed to decode gateway")
		return admission.Errored(http.StatusBadRequest, err)
	}

	mutate(gateway)

	jsonGateway, err := json.Marshal(gateway)
	if err != nil {
		log.WithFields(log.Fields{
			"name": gateway.Name,
			"err":  err,
		}).Error("failed to marshal gateway")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, jsonGateway)
}

func (g *GatewayMutationHook) InjectDecoder(d *admission.Decoder) error {
	g.decoder = d
	return nil
}

func credentialName(namespace, name string) string {
	randomStr := random.SecureString(credentialNameRandomStrLen)
	credentialName := fmt.Sprintf("%s-%s-%s", namespace, name, randomStr)

	if len(credentialName) > secretNameMaxLength {
		original := credentialName
		credentialName = credentialName[:secretNameMaxLength]
		log.Infof("truncating credentialName %q to %q", original, credentialName)
	}

	return credentialName
}

func mutate(gateway *v1beta1.Gateway) {
	if gateway == nil {
		return
	}

	for _, s := range gateway.Spec.Servers {
		if s.Tls == nil {
			continue
		}

		if s.Tls.Mode == networkingv1beta1.ServerTLSSettings_SIMPLE {
			newCredentialName := credentialName(gateway.Namespace, gateway.Name)
			s.Tls.CredentialName = newCredentialName

			log.WithFields(log.Fields{
				"gateway":                 gateway.Name,
				"original_CredentialName": s.Tls.CredentialName,
				"new_CredentialName":      newCredentialName,
			}).Info("mutated CredentialName")
		}
	}
}
