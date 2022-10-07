package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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
	externalDNS *ExternalDNSConfig
}

//ExternalDNSConfig passes configuration to the external DNS mutation behavior
type ExternalDNSConfig struct {
	enabled  bool
	target   string
	selector Selector
}

//Selector is a key value pair for matching annotations
type Selector struct {
	key   string
	value string
}

//SetEnabled set the endabled field to a bool value
func (edc *ExternalDNSConfig) SetEnabled(enabled bool) {
	edc.enabled = enabled
}

//SetTarget sets the target field to a string value
func (edc *ExternalDNSConfig) SetTarget(target string) {
	edc.target = target
}

//SetSelector sets the select field from a string value or returns an error
func (edc *ExternalDNSConfig) SetSelector(target string) error {

	v := strings.Split(target, "=")
	if len(v) < 2 {
		return fmt.Errorf("External DNS annotation selector parse error expected key=value got: %q", target)
	}
	edc.selector = Selector{
		key:   v[0],
		value: v[1],
	}
	return nil
}

func NewExternalDNSConfig() *ExternalDNSConfig {
	return &ExternalDNSConfig{
		selector: Selector{
			key:   v1beta1labels.DefaultGatewayAllowListAnnotation,
			value: v1beta1labels.DefaultGatewayAllowListAnnotationOverrideValue,
		},
	}
}

func NewGatewayMutationHook(client istioversionedclient.Interface, nsl corev1listers.NamespaceLister, opts ...OptionsFunc) *GatewayMutationHook {

	gmh := &GatewayMutationHook{
		istioClient: client,
		nsLister:    nsl,
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

	var ns *corev1.Namespace
	if g.externalDNS != nil && g.externalDNS.enabled {
		ns, err = g.nsLister.Get(gateway.Namespace)
		if err != nil {
			log.Error(err, fmt.Sprintf("failed to get namespace: %s", gateway.Namespace))
		}
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

func mutate(ctx context.Context, gateway *v1beta1.Gateway, externalDNS *ExternalDNSConfig, ns *corev1.Namespace) *v1beta1.Gateway {
	log := log.FromContext(ctx)

	if externalDNS != nil && externalDNS.enabled {
		externalDNS.mutate(ctx, gateway, ns)
	}

	//If we don't have the tls management label or it isn't set to true return
	if val, ok := gateway.Labels[v1beta1labels.InjectSimpleCredentialNameLabel]; ok && val == "true" {
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

func (edc *ExternalDNSConfig) mutate(ctx context.Context, gateway *v1beta1.Gateway, ns *corev1.Namespace) {

	//If we don't have information about the namespace assume we want to mutate it.
	if ns != nil {
		// if any host ingress is allowed in the namespace, do no mutation and return
		if allowed, ok := ns.Annotations[edc.selector.key]; ok && allowed == edc.selector.value {
			return
		}
	}

	if gateway.Annotations == nil {
		gateway.Annotations = map[string]string{}
	}

	// we only allow external-dns to use the hosts key on gateway server entries because those are validated by OPA
	delete(gateway.Annotations, v1beta1labels.ExternalDNSHostnameAnnotationKey)

	// set the target annotation if we have a target or delete it if we don't
	if edc.target != "" {
		if gateway.Annotations == nil {
			gateway.Annotations = map[string]string{}
		}
		gateway.Annotations[v1beta1labels.ExternalDNSTargetAnnotationKey] = edc.target
	} else {
		delete(gateway.Annotations, v1beta1labels.ExternalDNSTargetAnnotationKey)
	}
}
