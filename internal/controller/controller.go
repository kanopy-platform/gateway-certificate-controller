package controller

import (
	"context"
	"fmt"
	"time"

	certmanagerversionedclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned"
	istioversionedclient "istio.io/client-go/pkg/clientset/versioned"
	istioinformers "istio.io/client-go/pkg/informers/externalversions/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	log "github.com/sirupsen/logrus"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	istionetworkinglistersv1beta1 "istio.io/client-go/pkg/listers/networking/v1beta1"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"

	certbuilder "github.com/10gen/kanopy/pkg/builder/certmanager"
)

const controllerAgentName = "cert-gateway"

type Controller struct {
	certmanagerClientset certmanagerversionedclient.Interface
	istioClientset       istioversionedclient.Interface
	istioSynced          cache.InformerSynced
	gatewayLister        istionetworkinglistersv1beta1.GatewayLister

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
}

func New(
	certmanagerClientset certmanagerversionedclient.Interface,
	istioClientset istioversionedclient.Interface,
	istioInformer istioinformers.GatewayInformer) manager.Runnable {

	c := &Controller{
		certmanagerClientset: certmanagerClientset,
		istioClientset:       istioClientset,
		istioSynced:          istioInformer.Informer().HasSynced,
		gatewayLister:        istioInformer.Lister(),
		//certmanagerSynced:    certManagerInformer.Informer().HasSynced,
		workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Gateways"),
	}

	istioInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.handleObject,
		UpdateFunc: func(old, new interface{}) {
			c.handleObject(new)
		},
		DeleteFunc: c.handleObject,
	})

	return c
}

func (c *Controller) Start(ctx context.Context) error {
	return c.runWorkers(ctx, 2) // TODO configure num workers default with funcopts
}

func (c *Controller) runWorkers(ctx context.Context, workers int) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	log.Info("Starting Cert Gateway controller")
	log.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForNamedCacheSync(controllerAgentName, ctx.Done(), c.istioSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	log.Info("Starting workers")
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, ctx.Done())
	}

	log.Info("Started workers")
	<-ctx.Done()
	log.Info("Shutting down workers")

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)

		var key string
		var ok bool

		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		log.Infof("Successfully synced '%s'", key)

		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return false
}

func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	gateway, err := c.gatewayLister.Gateways(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			utilruntime.HandleError((fmt.Errorf("gateway '%s' in work queue no longer exists", key)))
			return nil
		}

		return err
	}

	for _, server := range gateway.Spec.Servers {
		if server.Tls == nil {
			continue
		}

		if server.Tls.Mode != networkingv1beta1.ServerTLSSettings_SIMPLE {
			continue
		}

		// find if the certificate exists
		_, err = c.certmanagerClientset.CertmanagerV1().Certificates("routing").Get(context.TODO(), server.Tls.CredentialName, metav1.GetOptions{})
		var create bool
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to fetch certificate: %v", err)
			}
			create = true
		}

		if create {
			cert := certbuilder.NewCertificate(server.Tls.CredentialName).WithSecretName(server.Tls.CredentialName).WithClusterIssuer("selfsigned").AppendDNSNames(server.Hosts...)
			log.Infof("creating cert %s", server.Tls.CredentialName)
			_, err := c.certmanagerClientset.CertmanagerV1().Certificates("routing").Create(context.TODO(), &cert.Certificate, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else {
			log.Infof("certificate for gateway: %s exists.", name)
		}
	}
	return nil
}

func (c *Controller) enqueueGateway(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}

func (c *Controller) handleObject(obj interface{}) {

	// process objects and enqueue gateways

	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}

		log.Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}

	log.Infof("Processing object: %s", object.GetName())

	gateway, err := c.gatewayLister.Gateways(object.GetNamespace()).Get(object.GetName())
	if err != nil {
		log.Infof("ignoring object '%s/%s", object.GetNamespace(), object.GetName())
		return
	}

	if len(gateway.Spec.Servers) <= 0 {
		log.Infof("gateway %s is misconfigured", object.GetName())
		return
	}

	server := gateway.Spec.Servers[0]
	if server.Tls == nil || server.Tls.CredentialName == "" {
		log.Infof("gateway %s has not configured TLs, skipping", object.GetName())
		return
	}

	c.enqueueGateway(gateway)
}
