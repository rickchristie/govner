package docker

import "strings"

var runtimeNamespace = "cooper"

// SetRuntimeNamespace sets the namespace used for Docker runtime resources
// such as proxy containers, networks, and barrel container names.
// The default namespace is "cooper", which preserves the production names
// cooper-proxy, cooper-internal, and cooper-external.
func SetRuntimeNamespace(namespace string) {
	namespace = strings.TrimSpace(namespace)
	namespace = strings.TrimSuffix(namespace, "-")
	if namespace == "" {
		namespace = "cooper"
	}
	runtimeNamespace = namespace
}

// RuntimeNamespace returns the current Docker runtime namespace.
func RuntimeNamespace() string {
	return runtimeNamespace
}

// ProxyContainerName returns the runtime name of the proxy container.
func ProxyContainerName() string {
	return runtimeNamespace + "-proxy"
}

// ExternalNetworkName returns the runtime name of the external Docker network.
func ExternalNetworkName() string {
	return runtimeNamespace + "-external"
}

// InternalNetworkName returns the runtime name of the internal Docker network.
func InternalNetworkName() string {
	return runtimeNamespace + "-internal"
}

// ProxyHost returns the hostname that barrels should use to reach the proxy
// container over Docker DNS. This matches the proxy container name.
func ProxyHost() string {
	return ProxyContainerName()
}

// BarrelNamePrefix returns the prefix used for barrel container names in the
// current runtime namespace.
func BarrelNamePrefix() string {
	if runtimeNamespace == "cooper" {
		return "barrel-"
	}
	return runtimeNamespace + "-barrel-"
}
