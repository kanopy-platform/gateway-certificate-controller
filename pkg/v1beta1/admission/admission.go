package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"
	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/random"
	logrus "github.com/sirupsen/logrus"
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
	log.Info("YUZHOU DEBUG req: %q", req)

	gateway := &v1beta1.Gateway{}

	err := g.decoder.Decode(req, gateway)
	if err != nil {
		log.Error(err, "failed to decode gateway")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if !shouldMutate(*gateway) {
		log.Info("skip mutating gateway: %s", gateway.Name)
		return admission.Allowed("gateway does not need mutation")
	}

	log.Info("mutating gateway: %s", gateway.Name)
	mutate(gateway)

	jsonGateway, err := json.Marshal(gateway)
	if err != nil {
		log.Error(err, "failed to marshal gateway")
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
		logrus.Infof("truncating credentialName %q to %q", original, credentialName)
	}

	return credentialName
}

func shouldMutate(g v1beta1.Gateway) bool {
	val, ok := g.Labels[labels.InjectSimpleCredentialNameLabel()]
	return ok && val == "true"
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
			s.Tls.CredentialName = credentialName(gateway.Namespace, gateway.Name)
		}
	}
}
