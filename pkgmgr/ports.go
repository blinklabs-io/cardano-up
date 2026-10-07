package pkgmgr

import (
	"errors"
	"net"
)

// Tracks host port allocations by context and package.
type PortRegistry map[string]ContextPortRegistry

// Maps packages to their service port mappings.
type ContextPortRegistry map[string]PackagePortRegistry

// Maps a service name to its container->host port pairs.
type PackagePortRegistry map[string]ServicePortMap

// ServicePortMap maps container port numbers (as strings) to host ports.
type ServicePortMap map[string]string

func cloneServicePortMap(src ServicePortMap) ServicePortMap {
	if src == nil {
		return nil
	}
	dst := make(ServicePortMap, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func clonePackagePortRegistry(src PackagePortRegistry) PackagePortRegistry {
	if len(src) == 0 {
		return nil
	}
	dst := make(PackagePortRegistry, len(src))
	for svc, ports := range src {
		dst[svc] = cloneServicePortMap(ports)
	}
	return dst
}

func reservedNativePorts(
	registry PortRegistry,
	currentContext string,
	currentPackage string,
) map[string]struct{} {
	ret := make(map[string]struct{})
	for contextName, contextRegistry := range registry {
		for packageName, packageRegistry := range contextRegistry {
			if contextName == currentContext && packageName == currentPackage {
				continue
			}
			for _, port := range packageRegistry[nativePortService] {
				if port != "" {
					ret[port] = struct{}{}
				}
			}
		}
	}
	return ret
}

func allocateNativePort(
	reserved map[string]struct{},
) (string, net.Listener, error) {
	for range 32 {
		// Reserve across interfaces because native services may bind beyond loopback.
		listener, err := net.Listen("tcp", ":0") // #nosec G102 -- reservation prevents port conflicts
		if err != nil {
			return "", nil, err
		}
		_, port, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			_ = listener.Close()
			return "", nil, err
		}
		if _, exists := reserved[port]; exists {
			_ = listener.Close()
			continue
		}
		reserved[port] = struct{}{}
		return port, listener, nil
	}
	return "", nil, errors.New("failed to allocate an unreserved native port")
}

func reserveNativePort(port string) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("", port))
}
