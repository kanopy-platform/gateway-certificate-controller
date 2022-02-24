package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"

	networkingv1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	admissionhook "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type GatewayMutationHook struct {
	decoder        *admission.Decoder
	istioClientset *istioversionedclient.Clientset
}

func GatewayMutationHandler(istioClientset *istioversionedclient.Clientset) admissionhook.Handler {
	gmh := &GatewayMutationHook{istioClientset: istioClientset}
	return gmh
}

func (gmh *GatewayMutationHook) Handle(ctx context.Context, req admissionhook.Request) admissionhook.Response {
	gateway := &v1beta1.Gateway{}

	err := gmh.decoder.Decode(req, gateway)
	if err != nil {
		log.Error("unable to decode gateway")
		return admission.Errored(http.StatusBadRequest, err)
	}

	log.Infof("attempting to mutate gateway: %s ", gateway.Name)

	// for each server, inspect tls and set the credentialName
	for i, s := range gateway.Spec.Servers {
		if s.Tls == nil {
			continue
		}

		if s.Tls.Mode == networkingv1beta1.ServerTLSSettings_SIMPLE {
			log.Infof("setting credentialName for gateway : %s on server %d", gateway.Name, i)
			s.Tls.CredentialName = fmt.Sprintf("%s-%s-%d", gateway.Namespace, gateway.Name, i)
		}
	}

	marshaledGateway, err := json.Marshal(gateway)
	if err != nil {
		log.Info("gateway: cannot marshal")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admissionhook.PatchResponseFromRaw(req.Object.Raw, marshaledGateway)
}

func (gmh *GatewayMutationHook) InjectDecoder(d *admission.Decoder) error {
	gmh.decoder = d
	return nil
}
