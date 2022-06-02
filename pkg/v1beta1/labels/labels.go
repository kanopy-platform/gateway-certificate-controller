package labels

import (
	"fmt"
	"strings"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/version"
	apilabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

type Label string

const (
	InjectSimpleCredentialNameLabel Label = "istio-cert-controller-inject-simple-credential-name"
	ManagedLabel                    Label = "istio-cert-controller-managed"
)

func InjectSimpleCredentialNameLabelSelector() string {
	return apilabels.Set(map[string]string{fmt.Sprintf("%s/%s", version.String(), string(InjectSimpleCredentialNameLabel)): "true"}).AsSelector().String()
}

func ManagedLabelString() string {
	return fmt.Sprintf("%s/%s", version.String(), string(ManagedLabel))
}

func ManagedLabelSelector() string {
	managedReq, err := apilabels.NewRequirement(ManagedLabelString(), selection.Exists, []string{})
	utilruntime.Must(err)

	return apilabels.NewSelector().Add(*managedReq).String()
}

func ParseManagedLabel(in string) (gateway string, namespace string) {
	s := strings.Split(in, ".")
	if len(s) < 2 {
		return "", ""
	}

	return s[0], s[1]
}
