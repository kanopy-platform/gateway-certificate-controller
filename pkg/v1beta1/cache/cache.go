package cache

import (
	"fmt"
	"strings"

	"sync"

	"github.com/go-logr/logr"
	v1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	klog "sigs.k8s.io/controller-runtime/pkg/log"
)

// GatewayLookupCache provides concurrency safe lookups from dns host to namespace/gateway
// Use New() as the Add, Delete, and Get functions all assume the cache map is non-nil
type GatewayLookupCache struct {
	cache  map[string]string
	mutex  sync.Mutex
	logger logr.Logger
}

func New() *GatewayLookupCache {
	return &GatewayLookupCache{
		cache:  make(map[string]string),
		mutex:  sync.Mutex{},
		logger: klog.Log,
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

	return
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
	glc.Delete(hosts...)
	return
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
	return
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

		if current[newVal] != true {
			adds = append(adds, newVal)
		}
	}

	for _, oldVal := range old {
		if current[oldVal] != true {
			dels = append(dels, oldVal)
		}
	}

	return adds, dels
}
