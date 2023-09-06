package cache

import (
	"fmt"
	"strings"

	"sync"

	v1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
)

func main() {
	fmt.Println("vim-go")
}

// GatewayLookupCache provides concurrency safe lookups from dns host to namespace/gateway
// Use New() as the Add, Delete, and Get functions all assume the cache map is non-nil
type GatewayLookupCache struct {
	cache map[string]string
	mutex sync.Mutex
}

func New() *GatewayLookupCache {
	return &GatewayLookupCache{
		cache: make(map[string]string),
		mutex: sync.Mutex{},
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

func (glc *GatewayLookupCache) AddFunc(obj interface{}) error {
	gw, ok := obj.(*v1beta1.Gateway)
	if !ok {
		return fmt.Errorf("Not a gateway.v1beta1.istio.io resource")

	}

	if gw == nil {
		return nil
	}

	namespacedName := fmt.Sprintf("%s/%s", gw.Namespace, gw.Name)
	hosts := gwToHosts(gw)
	glc.Add(namespacedName, hosts...)
	return nil
}
func (glc *GatewayLookupCache) DeleteFunc(obj interface{}) error {
	gw, ok := obj.(*v1beta1.Gateway)
	if !ok {
		return fmt.Errorf("Not a gateway.v1beta1.istio.io resource")

	}

	if gw == nil {
		return nil
	}

	hosts := gwToHosts(gw)
	glc.Delete(hosts...)
	return nil
}
func (glc *GatewayLookupCache) UpdateFunc(oldObj, newObj interface{}) error {
	oldGW, ok := oldObj.(*v1beta1.Gateway)
	if !ok {
		return fmt.Errorf("Old obj not a gateway.v1beta1.istio.io resource")

	}

	newGW, ok := newObj.(*v1beta1.Gateway)
	if !ok {
		return fmt.Errorf("New obj not a gateway.v1beta1.istio.io resource")

	}

	if oldGW == nil || newGW == nil {
		return nil
	}

	namespacedName := fmt.Sprintf("%s/%s", newGW.Namespace, newGW.Name)
	adds, deletes := diffSlices(gwToHosts(oldGW), gwToHosts(newGW))

	glc.Add(namespacedName, adds...)
	glc.Delete(deletes...)
	return nil
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

//diffSlices takes two slices an returns a list of additions and subtractions in the newer list
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
