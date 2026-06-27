# gopkg/consul does not provide consulting service since 2022/06

## Ordinary developers do not use the consul library

Service discovery is a basic component that does not associated with general developers. Most developers use service discovery indirectly through platforms such as TCE, RPC framework, Service Mesh, and FaaS.

Ordinary business developers can complete service discovery through components, which are actually implemented using Service Mesh or microservice framework, and do not need to directly call the sdk of service discovery.

If you need to customize the load balancing, then frameworks such as KiteX also provide a good ability to customize the load balancing algorithm, I believe you do not need to customize. Using `option.WithHost("IP:Port")` is also a very unrecommended usage, which increases the load balancing pressure, which is very dangerous.

TCE comes with the capability of service registration and health check, so ordinary developers do not need to use the library to complete service registration and health check.

## If you are the low-level component maintainer

The consul agent provides a very simple set of HTTP interfaces that are simple enough to be called directly.

The basic components may be C++/Java/Go and other languages. Directly using HTTP across languages ​​is effective, and problems are easy to troubleshoot.

The gopkg/consul library is implemented simply and stable for a long time, so you can use it, but if you have problems, please check the code yourself. We do not provide consulting service due to the lack of human resource.