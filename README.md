# Gateway Certificate Controller

A controller to automate the creation of cert-manager Certificates for istio Gateway resources.

## Directory and File Layout

| Directory | Description | 
| --------- | ----------- |
| examples/k8s | Example manifests which can be used in the development of the controller |
| internal/cli | Defines the command line interface and flags |
| internal/controllers | Versioned controller logic |
| internal/log | internal log configuration and helpers |
| internal/version | Version and build info set via ldflags |
| pkg/v1beta1 | Versioned packages for the controllers |
| pkg/v1beta1/labels | Defines labels used in kubernetes resource selectors |
| pkg/v1beta1/version | Version formatting and helpers |

## Development

Run `skaffold dev` to continously deploy into minikube / local k8s environment for testing. ([skaffold](https://skaffold.dev/))