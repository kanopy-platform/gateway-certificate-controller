package labels

import (
	"fmt"
	"strings"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1/version"
	apilabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

type Label string

var (
	InjectSimpleCredentialNameLabel     = fmt.Sprintf("%s/%s", version.String(), "istio-cert-controller-inject-simple-credential-name")
	IssuerAnnotation                    = fmt.Sprintf("%s/%s", version.String(), "istio-cert-controller-issuer")
	ManagedLabel                        = fmt.Sprintf("%s/%s", version.String(), "istio-cert-controller-managed")
	IssueTemporaryCertificateAnnotation = fmt.Sprintf("%s/%s", version.String(), IssueTemporaryCertificate)
	HTTPSolverAnnotation                = fmt.Sprintf("%s/%s", version.String(), HTTP01)
)

const (
	// ExternalDNSHostnameAnnotationKey the external-dns annotation for setting the host for record creation
	ExternalDNSHostnameAnnotationKey = "external-dns.alpha.kubernetes.io/hostname"

	// ExternalDNSTargetAnnotationKey the external-dns annotation for setting the target of the host record
	ExternalDNSTargetAnnotationKey = "external-dns.alpha.kubernetes.io/target"

	// DefaultGatewayAllowListAnnotation the default key for the label controlling external-dns mutation opt out
	DefaultGatewayAllowListAnnotation = "ingress-whitelist"

	// IssueTemporaryCertificate idicates that the cert-manager.io/issue-temporary-certificate annotation for a certificate should be set to true
	IssueTemporaryCertificate = "issue-temporary-certificate"

	// DefaultGatewayAllowListAnnotationOverrideValue the default value for the label controlling external-dns mutation opt out
	DefaultGatewayAllowListAnnotationOverrideValue = "*"

	// HTTP01
	HTTP01 = "http01"
)

func InjectSimpleCredentialNameLabelSelector() string {
	return apilabels.Set(map[string]string{InjectSimpleCredentialNameLabel: "true"}).AsSelector().String()
}

func ManagedLabelSelector() string {
	managedReq, err := apilabels.NewRequirement(ManagedLabel, selection.Exists, []string{})
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
