# Installation

The installation of the Istio Gateway Certificate Controller is by design up to the user.  The repository provides a sample deployment for [reference](../examples/k8s/deployment.yaml).

## Pre-Reqs

1. [Istio](https://istio.io/) Service Mesh
1. [Cert-Manager](https://cert-manager.io/) with at least one ClusterIssuer

## Flags

```
Flags:
      --as string                      Username to impersonate for the operation. User could be a regular user or a service account in a namespace.
      --as-group stringArray           Group to impersonate for the operation, this flag can be repeated to specify multiple groups.
      --as-uid string                  UID to impersonate for the operation.
      --cache-dir string               Default cache directory (default "/Users/david.katz/.kube/cache")
      --certificate-authority string   Path to a cert file for the certificate authority
      --certificate-namespace string   Namespace that stores Certificates (default "cert-manager")
      --client-certificate string      Path to a client certificate file for TLS
      --client-key string              Path to a client key file for TLS
      --cluster string                 The name of the kubeconfig cluster to use
      --context string                 The name of the kubeconfig context to use
      --default-issuer string          The default ClusterIssuer (default "selfsigned")
      --dry-run                        Controller dry-run changes only
  -h, --help                           help for kanopy-gateway-cert-controller
      --insecure-skip-tls-verify       If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure
      --kubeconfig string              Path to the kubeconfig file to use for CLI requests.
      --log-level string               Configure log level (default "info")
  -n, --namespace string               If present, the namespace scope for this CLI request
      --request-timeout string         The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests. (default "0")
  -s, --server string                  The address and port of the Kubernetes API server
      --tls-server-name string         Server name to use for server certificate validation. If it is not provided, the hostname used to contact the server is used
      --token string                   Bearer token for authentication to the API server
      --user string                    The name of the kubeconfig user to use
      --webhook-certs-dir string       Admission webhook TLS certificate directory (default "/etc/webhook/certs")
      --webhook-listen-port int        Admission webhook listen port (default 8443)
```

## RBAC

As provided within the [example rbac](../examples/k8s/rolebindings.yaml) this controller will need access to the following:

- Ability to watch/list/get/update/patch Gateways in all namespaces
- Full access to manage certificates within the `--certificate-namespace`
- Ability to create [leases](https://kubernetes.io/docs/reference/kubernetes-api/cluster-resources/lease-v1/) which the controller uses to manage leader election

## Metrics

The service uses port 80 to host prometheus metrics on `/metrics`

## Replicas

A minimum two replicas MAY be run in order to provide fault tolerance.  The controller uses leader election to verify that only one replica is active at a time.