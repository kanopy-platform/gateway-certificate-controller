package gateway

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/cache"
	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"

	certmanagerclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"istio.io/api/networking/v1beta1"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformers "istio.io/client-go/pkg/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8scache "k8s.io/client-go/tools/cache"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	v1certmanager "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	v1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

const (
	FieldManager = "isto-cert-controller"
)

type certificateHandler interface {
	CreateCertificate(ctx context.Context, gateway *networkingv1beta1.Gateway, server *v1beta1.Server) error
	UpdateCertificate(ctx context.Context, cert *v1certmanager.Certificate, gateway *networkingv1beta1.Gateway, server *v1beta1.Server) error
}

type GatewayController struct {
	istioClient          istioversionedclient.Interface
	certClient           certmanagerclient.Interface
	dryRun               bool
	name                 string
	certificateNamespace string
	clusterIssuer        string
	gatewayLookupCache   *cache.GatewayLookupCache
	certHandler          certificateHandler
	httpSolverLabel      string
}

func NewGatewayController(istioClient istioversionedclient.Interface, certClient certmanagerclient.Interface, opts ...OptionsFunc) *GatewayController {
	gr := &GatewayController{
		name:                 "istio-gateway-controller",
		istioClient:          istioClient,
		certClient:           certClient,
		certificateNamespace: "default",
		clusterIssuer:        "default",
	}

	for _, opt := range opts {
		opt(gr)
	}

	gr.certHandler = gr
	return gr
}

func (c *GatewayController) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	log := log.FromContext(ctx)

	ctrl, err := controller.New("istio-gateway-controller", mgr, controller.Options{
		Reconciler: c,
	})
	if err != nil {
		return err
	}

	istioInformerFactory := istioinformers.NewSharedInformerFactoryWithOptions(c.istioClient, time.Second*30)

	informer := istioInformerFactory.Networking().V1beta1().Gateways().Informer()
	if c.gatewayLookupCache != nil {
		_, err := informer.AddEventHandler(k8scache.ResourceEventHandlerFuncs{
			AddFunc:    c.gatewayLookupCache.AddFunc,
			UpdateFunc: c.gatewayLookupCache.UpdateFunc,
			DeleteFunc: c.gatewayLookupCache.DeleteFunc,
		})
		if err != nil {
			log.Error(err, "error adding event handler to gateway informer")
			return err
		}
	}

	if err := ctrl.Watch(&source.Informer{Informer: informer},
		&handler.EnqueueRequestForObject{}); err != nil {
		return err
	}

	istioInformerFactory.Start(ctx.Done())

	return nil
}

func (c *GatewayController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// set up a convenient log object so we don't have to type request over and over again
	log := log.FromContext(ctx)
	log.Info("Reconciling Gateway...", "reconcile", request.String())
	log.V(1).Info("Debug")
	gateway, err := c.istioClient.NetworkingV1beta1().Gateways(request.Namespace).Get(ctx, request.Name, metav1.GetOptions{})

	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil // garbage collection will handle
		}

		log.Error(err, "Error reconciling gateway, requeued")
		return reconcile.Result{
			Requeue: true,
		}, err
	}

	//If we don't have the tls management label or it isn't set to true return
	if val, ok := gateway.Labels[v1beta1labels.InjectSimpleCredentialNameLabel]; !ok || val != "true" {
		return reconcile.Result{}, nil
	}

	for _, s := range gateway.Spec.Servers {
		log.V(1).Info("Inspecting server", "hosts", s.Hosts)

		cert, err := c.certClient.CertmanagerV1().Certificates(c.certificateNamespace).Get(ctx, s.Tls.CredentialName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				if err := c.certHandler.CreateCertificate(ctx, gateway, s); err != nil {
					return reconcile.Result{
						Requeue: true,
					}, err
				}
			} else {
				return reconcile.Result{
					Requeue: true,
				}, err
			}
		} else {
			log.V(1).Info("Found certificate", "server", s)
			if err := c.certHandler.UpdateCertificate(ctx, cert.DeepCopy(), gateway, s); err != nil {
				return reconcile.Result{
					Requeue: true,
				}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

func (c *GatewayController) CreateCertificate(ctx context.Context, gateway *networkingv1beta1.Gateway, server *v1beta1.Server) error {
	log := log.FromContext(ctx)
	issuer := c.clusterIssuer

	if i, ok := gateway.Annotations[v1beta1labels.IssuerAnnotation]; ok {
		issuer = i
	}

	if server.Tls.Mode != v1beta1.ServerTLSSettings_SIMPLE {
		return nil
	}

	cert := &v1certmanager.Certificate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Certificate",
			APIVersion: "cert-manager.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        server.Tls.CredentialName,
			Labels:      map[string]string{v1beta1labels.ManagedLabel: fmt.Sprintf("%s.%s", gateway.Name, gateway.Namespace)},
			Annotations: map[string]string{},
		},
		Spec: v1certmanager.CertificateSpec{
			DNSNames:   getSortedHostsWithoutNamespace(server.Hosts),
			SecretName: server.Tls.CredentialName,
			IssuerRef: v1.ObjectReference{
				Kind:  "ClusterIssuer",
				Name:  issuer,
				Group: "cert-manager.io",
			},
		},
	}

	if b, ok := gateway.Annotations[v1beta1labels.IssueTemporaryCertificateAnnotation]; ok && b == "true" {
		cert.Annotations[v1certmanager.IssueTemporaryCertificateAnnotation] = "true"
	}

	if b, ok := gateway.Annotations[v1beta1labels.HTTPSolverAnnotation]; ok && b == "true" {
		cert.Labels[c.httpSolverLabel] = "true"
	}
	createOptions := metav1.CreateOptions{FieldManager: FieldManager}
	if c.dryRun {
		log.Info("[dryrun] create certificate", "cert", cert)
		createOptions.DryRun = []string{metav1.DryRunAll}
	}
	_, err := c.certClient.CertmanagerV1().Certificates(c.certificateNamespace).Create(ctx, cert, createOptions)
	return err
}

func getSortedHostsWithoutNamespace(serverHosts []string) []string {
	hosts := make([]string, len(serverHosts))

	for i, h := range serverHosts {
		parts := strings.Split(h, "/")
		hosts[i] = h
		if len(parts) > 1 {
			hosts[i] = parts[1]
		}
	}
	sort.Strings(hosts)
	return hosts
}

func (c *GatewayController) UpdateCertificate(ctx context.Context, cert *v1certmanager.Certificate, gateway *networkingv1beta1.Gateway, server *v1beta1.Server) error {
	log := log.FromContext(ctx)

	cert, updatedIssuer := updateCertificateIssuer(ctx, cert, gateway)
	cert, updatedDNSNames := updateCertificateDNSNames(ctx, cert, server)
	cert, updatedHTTPSolver := updateHTTPSolver(ctx, cert, gateway, c.httpSolverLabel)

	if updatedDNSNames || updatedIssuer || updatedHTTPSolver {
		log.V(1).Info("pre-update", "cert", cert)

		updateOptions := metav1.UpdateOptions{FieldManager: FieldManager}
		if c.dryRun {
			log.Info("[dryrun] update certificate", "cert", cert)
			updateOptions.DryRun = []string{metav1.DryRunAll}
		}

		_, err := c.certClient.CertmanagerV1().Certificates(c.certificateNamespace).Update(ctx, cert, updateOptions)
		if err != nil {
			log.Error(err, "error on certificate update", "cert", cert)
		}
		return err
	}

	return nil
}
func updateHTTPSolver(ctx context.Context, cert *v1certmanager.Certificate, gateway *networkingv1beta1.Gateway, label string) (*v1certmanager.Certificate, bool) {
	log := log.FromContext(ctx)

	if h, ok := gateway.Annotations[v1beta1labels.HTTPSolverAnnotation]; ok && h == "true" {
		if cert.Labels == nil {
			cert.Labels = map[string]string{}
		}

		if l, ok := cert.Labels[label]; !ok || (ok && l != "true") {
			log.V(1).Info("Adding http solver label to cert")
			cert.Labels[label] = "true"
			return cert, true
		} else {
			return cert, false
		}
	}

	if l, ok := cert.Labels[label]; ok && l == "true" {
		log.V(1).Info("Removing http solver label from cert")
		delete(cert.Labels, label)
		return cert, true
	}

	return cert, false

}
func updateCertificateIssuer(ctx context.Context, cert *v1certmanager.Certificate, gateway *networkingv1beta1.Gateway) (*v1certmanager.Certificate, bool) {
	log := log.FromContext(ctx)
	issuer := cert.Spec.IssuerRef.Name

	if i, ok := gateway.Annotations[v1beta1labels.IssuerAnnotation]; ok {
		log.V(1).Info("got issuer from annotation", "issuer", i)
		issuer = i
	}

	updated := cert.Spec.IssuerRef.Name != issuer
	cert.Spec.IssuerRef.Name = issuer
	return cert, updated
}

func updateCertificateDNSNames(ctx context.Context, cert *v1certmanager.Certificate, server *v1beta1.Server) (*v1certmanager.Certificate, bool) {
	hosts := getSortedHostsWithoutNamespace(server.Hosts)
	updated := !reflect.DeepEqual(hosts, cert.Spec.DNSNames)
	cert.Spec.DNSNames = hosts
	return cert, updated
}
