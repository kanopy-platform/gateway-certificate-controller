# Gateway Controller

The purpose of the controller is to watch [labeled](../api/v1beta1.md) Gateway resources and construct Certificates within the defined `CertificateNamespace`.

- Istio maintains a validation constraint that enforces a servers `port.name` must be unique within the slice.

## Controller Reconcile Logic

- Given a Gateway [labeled](../api/v1beta1.md) for management by the controller.
- Inspect each Server entry and check if a Certificate exists.
- If not exists, Create the Certificate if `tls.Mode = SIMPLE`
- If exists, Update the Certificate with the server's hosts slice.

For example:

```yaml
---
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: httpbin-gateway
  namespace: default
  labels:
    "v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name": "true"
spec:
  selector:
    istio: ingressgateway
  servers:
    - port:
        number: 443
        name: https
        protocol: HTTPS
      hosts:
        - "default/httpbin.example.com"
      tls:
        mode: SIMPLE
```

- If the Gateway is annotated with `v1beta1.kanopy-platform.github.io/istio-cert-controller-issuer` the controller will set the ClusterIssuer accordingly.  The controller WILL NOT verify that the ClusterIssuer exists.

The Gateway above will yield the following Certificate:

```yaml
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: default-httpbin-gateway-https
  namespace: certificateNamespace
spec:
  dnsNames:
  - 'httpbin.example.com'
  issuerRef:
    group: cert-manager.io
    kind: ClusterIssuer
    name: selfsigned
  secretName: default-httpbin-gateway-https
```

Note, any Istio supported `namespace` prefix (`default/httpbin.example.com`) is removed before creating the certificate.
