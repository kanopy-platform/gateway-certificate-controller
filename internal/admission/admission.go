package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"
)

const (
	secretNameMaxLength           = 253
	credentialNameRandomStrLen    = 10
	lastAppliedMutationAnnotation = "v1beta1.kanopy-platform.github.io/last-applied-mutation"
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

func getGatewayServerByPortName(name string, gateway *v1beta1.Gateway) *networkingv1beta1.Server {
	for _, s := range gateway.Spec.Servers {
		if s.Port.Name == name {
			return s
		}
	}
	return nil
}

func getLastAppliedGateway(gateway *v1beta1.Gateway) (*v1beta1.Gateway, error) {
	var lastAppliedGateway *v1beta1.Gateway = nil
	if gateway.Annotations != nil {
		if js, ok := gateway.Annotations[lastAppliedMutationAnnotation]; ok {
			lastAppliedGateway = &v1beta1.Gateway{}
			if err := yaml.Unmarshal([]byte(js), lastAppliedGateway); err != nil {
				return nil, err
			}
		}
	}
	return lastAppliedGateway, nil
}

func annotateMutation(gateway *v1beta1.Gateway) (*v1beta1.Gateway, error) {
	ncs, err := yaml.Marshal(gateway)
	if err != nil {
		return nil, err
	}
	jsb, err := yaml.YAMLToJSON(ncs)

	if err != nil {
		return nil, err
	}

	if gateway.Annotations == nil {
		gateway.Annotations = map[string]string{}
	}

	gateway.Annotations[lastAppliedMutationAnnotation] = string(jsb)
	return gateway, nil
}

func mutate(ctx context.Context, gateway *v1beta1.Gateway) (*v1beta1.Gateway, error) {
	log := log.FromContext(ctx)

	lastAppliedGateway, err := getLastAppliedGateway(gateway)
	if err != nil {
		return nil, err
	}

	annotateLastApplied := false

	for _, s := range gateway.Spec.Servers {
		if s.Tls == nil {
			continue
		}

		if s.Tls.Mode == networkingv1beta1.ServerTLSSettings_SIMPLE {
			var shouldMutate bool = true

			if lastAppliedGateway != nil {
				if cs := getGatewayServerByPortName(s.Port.Name, lastAppliedGateway); cs != nil {
					shouldMutate = false
					s.Tls.CredentialName = cs.Tls.CredentialName
				}
			}

			if shouldMutate {
				annotateLastApplied = true
				newCredentialName := credentialName(ctx, gateway.Namespace, gateway.Name)
				log.Info(fmt.Sprintf("mutating gateway %s Tls.CredentialName, %s to %s", gateway.Name, s.Tls.CredentialName, newCredentialName))
				s.Tls.CredentialName = newCredentialName
			}
		}
	}

	if annotateLastApplied {
		gateway, err = annotateMutation(gateway)
		if err != nil {
			return nil, err
		}
	}
	return gateway, nil
}
