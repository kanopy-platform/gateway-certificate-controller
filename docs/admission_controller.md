# Addmission Controller

This service runs a mutating webhook on `/mutate`.

## Mutation Logic

- Given a Gateway [labeled](./api/v1beta1.md) for management by the controller.
- Inspect each Server entry.
- For each server that sets `tls.mode = SIMPLE` construct a `tls.credentialName` using the following format: `<namespace>-<gateway name>-<port-name>`

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

The mutated object will contain the following `tls.credentialName=default-httpbin-gateway-https`.

Since the `tls.credentialName` is used to name the `Certificate` and `Secret` resources it is subject to the [253 max character limit](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names).  The `<namespace>-<gateway-name>` will be truncated accordingly to preserve the `portName`