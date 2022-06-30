# Garbage Collection Controller

The purpose of the garbage collection controller is to add a degree of fault tolerance within this service.  Its job is to watch [managed](../api/v1beta1.md) Certificate resources and verify that the Gateway resource still exists.

- Gateway resources are treated as `user` managed and can therefore be deleted or updated at any time.
- The garbage collection service prevents orphaned Certificate resources in the event a Gateway is deleted.

## Reconcile Logic

- For all [managed](../api/v1beta1.md) Certificate Resources
- Inspect the associated Gateway
- If NOT exists:
  - Delete the Certificate.
- If exists:
  - Delete the Certificate if it is not in use. This occurs when the port.name is updated by the user.