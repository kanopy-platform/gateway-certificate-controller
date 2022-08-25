package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	secretNameMaxLength = 253
)

type GatewayMutationHook struct {
	istioClient istioversionedclient.Interface
	nsLister    corev1listers.NamespaceLister
	decoder     *admission.Decoder
	externalDNS *externalDNSConfig
}

type externalDNSConfig struct {
	enabled bool
	target  string
}

func NewGatewayMutationHook(client istioversionedclient.Interface, opts ...OptionsFunc) *GatewayMutationHook {
	gmh := &GatewayMutationHook{
		istioClient: client,
	}

	for _, opt := range opts {
		opt(gmh)
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

	ns, err := g.nsLister.Get(gateway.Namespace)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to get namespace: %s", gateway.Namespace))
	}
	gateway = mutate(ctx, gateway.DeepCopy(), g.externalDNS, ns)

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
	// Leave enough space for dash before the portName suffix.
	maxPrefixLen := secretNameMaxLength - len(portName) - 1

	if len(prefix) > maxPrefixLen {
		prefix = prefix[:maxPrefixLen]
		log.Info(fmt.Sprintf("truncating gateway %s credentialName to %s", name, prefix))
	}

	return fmt.Sprintf("%s-%s", prefix, portName)
}

func mutate(ctx context.Context, gateway *v1beta1.Gateway, externalDNS *externalDNSConfig, ns *corev1.Namespace) *v1beta1.Gateway {
	log := log.FromContext(ctx)

	if externalDNS != nil && externalDNS.enabled {
		mutateExternalDNSAnnotations(ctx, gateway, externalDNS.target, ns)
	}

	//If we don't have the tls management label or it isn't set to true return
	if val, ok := gateway.Labels[v1beta1labels.InjectSimpleCredentialNameLabel]; !ok || val != "true" {
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
	}

	return gateway
}

func mutateExternalDNSAnnotations(ctx context.Context, gateway *v1beta1.Gateway, target string, ns *corev1.Namespace) {

	if gateway == nil || ns == nil {
		return
	}

	// if any host ingress is allowed in the namespace, do no mutation and return
	if allowed, ok := ns.Labels[v1beta1labels.IngressAllowListLabel]; ok && allowed == "*" {
		return
	}

	// we only allow external-dns to use the hosts key on gateway server entries because those are validated by OPA
	delete(gateway.Annotations, v1beta1labels.ExternalDNSHostnameAnnotationKey)

	// set the target annotation if we have a target or delete it if we don't
	if target != "" {
		gateway.Annotations[v1beta1labels.ExternalDNSTargetAnnotationKey] = target
	} else {
		delete(gateway.Annotations, v1beta1labels.ExternalDNSHostnameAnnotationKey)
	}
}
