# Playbook: Migrating a Vanity Hostname from Traefik to Istio

This playbook guides an operator through migrating a vanity hostname from the
legacy traefik ingress controller to Istio Gateway-backed routing with zero
downtime.

## Background

The `gateway-certificate-controller` manages TLS certificates for Istio
`Gateway` resources. When a `Gateway` is created the controller issues a
cert-manager `Certificate`, which triggers an ACME order. How the resulting
HTTP-01 challenge is solved depends on whether DNS already resolves to Istio.

Two hostname classes exist:

| Class | DNS manager | ACME solver | Migration action |
|---|---|---|---|
| `<cluster>.corp.example.com` subdomains managed by external-dns | external-dns | DNS-01 (automatic) | None — proceeds without intervention |
| Hostnames not managed by external-dns | Manual / out-of-band | HTTP-01 (requires reachable ingress) | Requires Steps 2–4 below |

During the migration window DNS still resolves to the legacy traefik ingress
controller, so Istio VirtualService routing for ACME challenges has no effect.
The `ingress-http01` annotation described in Step 3 signals the controller to
create a Kubernetes `Ingress` instead so traefik can proxy the challenge request.

---

## Prerequisites

- `kubectl` access to the target cluster with permissions to edit `Gateway`
  resources and read `Certificate` / `Challenge` resources.
- The `gateway-certificate-controller` deployed with `--challenge-solver` and
  `--ingress-class=traefik` enabled.
- The vanity hostname's `Gateway` resource already exists (or will be created
  as part of the migration).

---

## Step 1 — Create or verify the Istio Gateway

Ensure the `Gateway` resource exists and is annotated with
`v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name: "true"`.
The controller ignores gateways without this label.

```yaml
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: login-gateway
  namespace: routing
  labels:
    v1beta1.kanopy-platform.github.io/istio-cert-controller-inject-simple-credential-name: "true"
  annotations:
    # Step 3 annotation added here before DNS cut-over (see below)
spec:
  selector:
    istio: ingressgateway
  servers:
  - hosts:
    - routing/login.corp.example.com
    port:
      name: https
      number: 443
      protocol: HTTPS
    tls:
      mode: SIMPLE
      credentialName: login-corp-tls
```

## Step 2 — Identify the hostname class

```bash
# DNS-01 path (no action required for challenge solving):
#   hostname is managed by external-dns (e.g. a subdomain of a delegated zone)

# HTTP-01 path (Step 3 required):
#   hostname is not managed by external-dns and DNS-01 is unavailable
#   e.g. login.corp.example.com
```

For DNS-01 hostnames skip to Step 4.

## Step 3 — Enable Ingress-based HTTP-01 challenge solving (HTTP-01 only)

Add the `ingress-http01` annotation to the Gateway **before** DNS cut-over.
This tells the controller to create a traefik `Ingress` for the ACME challenge
path instead of a VirtualService, so challenges can be answered while DNS still
resolves to traefik.

```bash
kubectl annotate gateway login-gateway \
  -n routing \
  v1beta1.kanopy-platform.github.io/ingress-http01=true
```

Verify the annotation is present and an `Ingress` is created when cert-manager
issues a challenge:

```bash
# Watch for the Certificate and its status
kubectl get certificate login-corp-tls -n cert-manager -w

# Confirm the Ingress was created (same name/namespace as the Challenge)
kubectl get ingress -n routing

# Confirm migration-mode certificates are queryable via the label
kubectl get certificates -n cert-manager -l use-ingress-http01-solver=true
```

The `Ingress` carries an owner reference pointing to the cert-manager
`Challenge` object and is automatically deleted when cert-manager removes the
Challenge after a successful issuance.

### Domain classification recap

| Annotation state | Plugin used | Routing resource created |
|---|---|---|
| Absent (normal) | `VirtualServicePlugin` | `VirtualService` |
| `ingress-http01: "true"` (migration) | `IngressPlugin` | `Ingress` (ingressClass: traefik) |

## Step 4 — Cut over DNS

Update the DNS record for the vanity hostname to point to the Istio ingress
load balancer IP/hostname.

```bash
# Confirm the certificate is Ready before cutting DNS
kubectl get certificate login-corp-tls -n cert-manager
# STATUS: Ready

# Then update DNS to point to the Istio ingress LB
```

## Step 5 — Remove the ingress-http01 annotation

Once DNS resolves to Istio and the certificate is valid, remove the annotation
so future renewals use the VirtualService path:

```bash
kubectl annotate gateway login-gateway \
  -n routing \
  v1beta1.kanopy-platform.github.io/ingress-http01-
```

After the next reconcile the controller removes the `use-ingress-http01-solver`
label from the Certificate and routes future ACME challenges through Istio.

> **Edge case:** If the annotation is removed while an ACME challenge is
> in-flight, the existing `Ingress` persists for the remainder of that
> challenge's lifecycle (it is owned by the `Challenge`, not the annotation).
> A VirtualService will be created at the next reconcile. Both resources coexist
> until the `Challenge` is deleted; the `Ingress` solves the challenge since DNS
> still resolves to traefik at that point. Avoid removing the annotation
> mid-challenge when possible.

## Step 6 — Clean up legacy traefik resources

Remove the vanity hostname's traefik `IngressRoute` (or equivalent) after
confirming traffic is flowing through Istio.

---

## Troubleshooting

| Symptom | Likely cause | Resolution |
|---|---|---|
| Certificate stays `Pending` with HTTP-01 challenge | `ingress-http01` annotation not set; VirtualService created but DNS still resolves to traefik | Add the annotation per Step 3 |
| Ingress not created after annotation | `--challenge-solver` flag not enabled on the controller | Enable the flag and restart the controller |
| `use-ingress-http01-solver` label missing from Certificate | `--ingress-solver-label` flag not matching expected value | Verify flag value with `kubectl describe pod -n <controller-ns>` |
| Challenge solved but cert not renewed after DNS cut-over | Annotation still present; controller still creates Ingress instead of VirtualService | Remove the annotation per Step 5 |
