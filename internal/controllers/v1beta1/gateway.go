package v1beta1

import (
	"context"
	"fmt"
	"sort"
	"time"

	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"

	certmanagerclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"istio.io/api/networking/v1beta1"
	networkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformers "istio.io/client-go/pkg/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

type eventReconciler interface {
	Cleanup(ctx context.Context, request reconcile.Request) error
	CreateCertificate(ctx context.Context, gateway *networkingv1beta1.Gateway, server *v1beta1.Server) error
	UpdateCertificate(ctx context.Context, cert *v1certmanager.Certificate, gateway *networkingv1beta1.Gateway, server *v1beta1.Server) error
}

type GatewayController struct {
	istioClient          istioversionedclient.Interface
	certClient           certmanagerclient.Interface
	name                 string
	certificateNamespace string
	events               eventReconciler
}

func NewGatewayController(istioClient istioversionedclient.Interface, certClient certmanagerclient.Interface, certNamespace string) *GatewayController {
	gr := &GatewayController{
		name:                 "istio-gateway-controller",
		istioClient:          istioClient,
		certClient:           certClient,
		certificateNamespace: certNamespace,
	}

	gr.events = gr
	return gr
}

func (c *GatewayController) SetupWithManager(ctx context.Context, mgr manager.Manager) error {
	ctrl, err := controller.New("istio-gateway-controller", mgr, controller.Options{
		Reconciler: c,
	})
	if err != nil {
		return err
	}

	istioInformerFactory := istioinformers.NewSharedInformerFactoryWithOptions(c.istioClient, time.Second*30, istioinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
		listOptions.LabelSelector = v1beta1labels.InjectSimpleCredentialNameLabelSelector()
	}))

	if err := ctrl.Watch(&source.Informer{Informer: istioInformerFactory.Networking().V1beta1().Gateways().Informer()},
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
			// check and delete cert
			// TODO, this can be left up to garbage collection
			log.Info("Cleaned up certificates")
			if err := c.events.Cleanup(ctx, request); err != nil {
				return reconcile.Result{Requeue: true}, err
			}
			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reconciling gateway, requeued")
		return reconcile.Result{
			Requeue: true,
		}, err
	}

	for _, s := range gateway.Spec.Servers {
		log.V(1).Info("Inspecting server", "hosts", s.Hosts)

		cert, err := c.certClient.CertmanagerV1().Certificates(c.certificateNamespace).Get(ctx, s.Tls.CredentialName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				if err := c.events.CreateCertificate(ctx, gateway, s); err != nil {
					return reconcile.Result{
						Requeue: true,
					}, err
				}
			} else {
				return reconcile.Result{
					Requeue: true,
				}, err
			}
		}

		if cert != nil {
			log.V(1).Info("Found certificate", "server", s)
			if err := c.events.UpdateCertificate(ctx, cert.DeepCopy(), gateway, s); err != nil {
				return reconcile.Result{
					Requeue: true,
				}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

func (c *GatewayController) Cleanup(ctx context.Context, request reconcile.Request) error {
	return nil
}

func (c *GatewayController) CreateCertificate(ctx context.Context, gateway *networkingv1beta1.Gateway, server *v1beta1.Server) error {
	issuer := "default"

	if i, ok := gateway.Annotations["v1beta1.kanopy-platform.github.io/istio-cert-controller-issuer"]; ok {
		issuer = i
	}

	if server.Tls.Mode != v1beta1.ServerTLSSettings_SIMPLE {
		return nil
	}

	sort.Strings(server.Hosts)

	cert := &v1certmanager.Certificate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Certificate",
			APIVersion: "cert-manager.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   server.Tls.CredentialName,
			Labels: map[string]string{"v1beta1.kanopy-platform.github.io/istio-cert-controller-managed": "true"},
			Annotations: map[string]string{
				"v1beta1.kanopy-platform.github.io/istio-cert-controller-managed": fmt.Sprintf("%s.%s", gateway.Name, gateway.Namespace),
			},
		},
		Spec: v1certmanager.CertificateSpec{
			DNSNames:   server.Hosts,
			SecretName: server.Tls.CredentialName,
			IssuerRef: v1.ObjectReference{
				Kind:  "ClusterIssuer",
				Name:  issuer,
				Group: "cert-manager.io",
			},
		},
	}

	_, err := c.certClient.CertmanagerV1().Certificates(c.certificateNamespace).Create(ctx, cert, metav1.CreateOptions{FieldManager: "istio-cert-controller"})
	return err
}

func (c *GatewayController) UpdateCertificate(ctx context.Context, cert *v1certmanager.Certificate, gateway *networkingv1beta1.Gateway, server *v1beta1.Server) error {
	log := log.FromContext(ctx)
	sort.Strings(server.Hosts)
	cert.Spec.DNSNames = server.Hosts

	issuer := cert.Spec.IssuerRef.Name
	log.V(1).Info("gateway", "annotations", gateway.Annotations)

	if i, ok := gateway.Annotations["v1beta1.kanopy-platform.github.io/istio-cert-controller-issuer"]; ok {
		log.V(1).Info("got issuer from annotation", "issuer", i)
		issuer = i
	}

	cert.Spec.IssuerRef.Name = issuer

	log.V(1).Info("pre-update", "cert", cert)
	fmt.Printf("%#v\n", cert)

	cu, err := c.certClient.CertmanagerV1().Certificates(c.certificateNamespace).Update(ctx, cert, metav1.UpdateOptions{FieldManager: "isto-cert-controller"})
	fmt.Println("err: ", err)
	fmt.Printf("%#v\n", cu)
	if err != nil {
		log.Error(err, "error on certificate update", "cert", cert)
	}
	return err
}
