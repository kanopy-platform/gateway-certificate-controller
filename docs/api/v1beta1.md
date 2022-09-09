# API - v1beta1

## Server TLS

The gateway and admission controllers will only mutate TLS and manage certificates for [v1beta1.Gateway](https://pkg.go.dev/istio.io/api/networking/v1beta1#Gateway) resources that are labeled with the following:

```yaml
labels:
    "v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name": "true"
```


When this label is set the controller will take over the TLS.CredentialName and install a certificate according to the default issuer set during [Installation](../installation.md)

A custom [ClusterIssuers](https://pkg.go.dev/github.com/jetstack/cert-manager/pkg/apis/certmanager/v1#ClusterIssuer) installed in your kubernetes cluster may be used per gateway with the annotation:

```yaml
annotations:
    v1beta1.kanopy-platform.github.io/istio-cert-controller-issuer: my-cluster-issuer
```

## Certificates

Certificates created by this controller will contain the following `Managed` label.  Following standard controller convention, certificates with this label SHOULD NOT be manually edited.

The value of this label will consist of two parts:
 1. The resource name of the gateway
 1. The namespace where the gateway belongs

```yaml
labels:
    v1beta1.kanopy-platform.github.io/istio-cert-controller-managed: name-of-gateway.namespace
```
