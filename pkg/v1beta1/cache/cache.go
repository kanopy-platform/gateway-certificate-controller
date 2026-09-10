package cache

import (
	"fmt"
	"strings"

	"sync"

	"github.com/go-logr/logr"
	v1beta1labels "github.com/kanopy-platform/gateway-certificate-controller/pkg/v1beta1/labels"
	v1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	klog "sigs.k8s.io/controller-runtime/pkg/log"
)

// GatewayLookupCache provides concurrency-safe lookups from DNS hostname to
// namespace/gateway-name. Use New() — Add, Delete, Get, SetIngressSolver, and
// GetIngressSolver all assume the internal maps are non-nil.
type GatewayLookupCache struct {
	cache              map[string]string
	ingressSolverHosts map[string]bool
	mutex              sync.Mutex
	logger             logr.Logger
}

func New() *GatewayLookupCache {
	return &GatewayLookupCache{
		cache:              make(map[string]string),
		ingressSolverHosts: make(map[string]bool),
		mutex:              sync.Mutex{},
		logger:             klog.Log,
	}
}

func (glc *GatewayLookupCache) Add(gateway string, hosts ...string) {
	glc.mutex.Lock()
	defer glc.mutex.Unlock()

	for _, host := range hosts {
		glc.cache[host] = gateway
	}
}

func (glc *GatewayLookupCache) Delete(hosts ...string) {
	glc.mutex.Lock()
	defer glc.mutex.Unlock()

	for _, host := range hosts {
		delete(glc.cache, host)
	}
}

func (glc *GatewayLookupCache) Get(host string) (string, bool) {
	glc.mutex.Lock()
	defer glc.mutex.Unlock()

	gw, ok := glc.cache[host]
	return gw, ok
}

// SetIngressSolver marks or unmarks a hostname as requiring the Ingress-based
// HTTP-01 solver. When enabled is false the entry is removed so that
// GetIngressSolver returns false for unknown hosts.
func (glc *GatewayLookupCache) SetIngressSolver(host string, enabled bool) {
	glc.mutex.Lock()
	defer glc.mutex.Unlock()

	if enabled {
		glc.ingressSolverHosts[host] = true
	} else {
		delete(glc.ingressSolverHosts, host)
	}
}

// GetIngressSolver reports whether the given hostname is currently flagged for
// Ingress-based HTTP-01 solving (i.e. its Gateway carries the
// ingress-http01 annotation set to "true").
func (glc *GatewayLookupCache) GetIngressSolver(host string) bool {
	glc.mutex.Lock()
	defer glc.mutex.Unlock()

	return glc.ingressSolverHosts[host]
}

func (glc *GatewayLookupCache) AddFunc(obj interface{}) {
	gw, ok := obj.(*v1beta1.Gateway)
	if !ok {
		glc.logger.V(1).Info("Not a gateway.v1beta1.istio.io resource")
		return
	}

	if gw == nil {
		return
	}

	namespacedName := fmt.Sprintf("%s/%s", gw.Namespace, gw.Name)
	hosts := gwToHosts(gw)
	glc.Add(namespacedName, hosts...)

	ingressSolver := gw.Annotations[v1beta1labels.IngressHTTPSolverAnnotation] == "true"
	for _, host := range hosts {
		glc.SetIngressSolver(host, ingressSolver)
	}
}

func (glc *GatewayLookupCache) DeleteFunc(obj interface{}) {
	gw, ok := obj.(*v1beta1.Gateway)
	if !ok {
		glc.logger.V(1).Info("Not a gateway.v1beta1.istio.io resource")
		return
	}

	if gw == nil {
		return
	}

	hosts := gwToHosts(gw)
	for _, host := range hosts {
		glc.SetIngressSolver(host, false)
	}
	glc.Delete(hosts...)
}

func (glc *GatewayLookupCache) UpdateFunc(oldObj, newObj interface{}) {
	oldGW, ok := oldObj.(*v1beta1.Gateway)
	if !ok {
		glc.logger.V(1).Info("Not a gateway.v1beta1.istio.io resource")
		return
	}

	newGW, ok := newObj.(*v1beta1.Gateway)
	if !ok {
		glc.logger.V(1).Info("Not a gateway.v1beta1.istio.io resource")
		return
	}

	if oldGW == nil || newGW == nil {
		return
	}

	namespacedName := fmt.Sprintf("%s/%s", newGW.Namespace, newGW.Name)
	adds, deletes := diffSlices(gwToHosts(oldGW), gwToHosts(newGW))

	glc.Add(namespacedName, adds...)
	glc.Delete(deletes...)

	// Sync the ingress-solver flag for all current hosts of the updated gateway.
	ingressSolver := newGW.Annotations[v1beta1labels.IngressHTTPSolverAnnotation] == "true"
	for _, host := range gwToHosts(newGW) {
		glc.SetIngressSolver(host, ingressSolver)
	}
	// Clear the flag for hosts that were removed.
	for _, host := range deletes {
		glc.SetIngressSolver(host, false)
	}
}

func gwToHosts(gw *v1beta1.Gateway) []string {
	hosts := []string{}
	if gw == nil {
		return hosts
	}

	for _, server := range gw.Spec.Servers {
		for _, host := range server.Hosts {
			// wildcard certificates cannot be solved via http-01
			if strings.Contains(host, "*") {
				continue
			}

			// split hosts in the namespace/dns.host.name format
			out, post, ok := strings.Cut(host, "/")
			if ok {
				out = post
			}
			hosts = append(hosts, out)
		}
	}

	return hosts
}

// diffSlices takes two slices an returns a list of additions and subtractions in the newer list
func diffSlices(old, newer []string) ([]string, []string) {
	adds, dels := []string{}, []string{}

	current := map[string]bool{}

	for _, newVal := range newer {
		for _, oldVal := range old {
			if oldVal == newVal {
				current[newVal] = true
				break
			}
		}

		if !current[newVal] {
			adds = append(adds, newVal)
		}
	}

	for _, oldVal := range old {
		if !current[oldVal] {
			dels = append(dels, oldVal)
		}
	}

	return adds, dels
}
