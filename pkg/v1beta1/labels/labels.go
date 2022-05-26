package labels

import (
	"fmt"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/version"
	apilabels "k8s.io/apimachinery/pkg/labels"
)

type Label string

const InjectSimpleCredentialName Label = "istio-cert-controller-inject-simple-credential-name"

func InjectSimpleCredentialNameLabel() string {
	return fmt.Sprintf("%s/%s", version.String(), string(InjectSimpleCredentialName))
}

func InjectSimpleCredentialNameLabelSelector() string {
	return apilabels.Set(map[string]string{fmt.Sprintf("%s/%s", version.String(), string(InjectSimpleCredentialName)): "true"}).AsSelector().String()
}
