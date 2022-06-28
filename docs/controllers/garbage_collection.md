# Garbage Collection Controller

The purpose of the garbage collection controller is to add a degree of fault tolerance within this service.  It's job is to watch [managed](../api/v1beta1.md) Certificate resources and verify that the Gateway resource still exists.

- Gateway resources are treated as `user` managed and can therefore be deleted or change configuration at any time.
- The garbage collection service prevents orphaned Certificate resources in the event a Gateway is deleted and this service for any reason misses that notification.

## Reconcile Logic

- For all [managed](../api/v1beta1.md) Certificate Resources
- Inspect the associated Gateway
- If exists, continue
- If NOT exists, delete the Certificate.