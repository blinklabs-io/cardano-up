package pkgmgr

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
