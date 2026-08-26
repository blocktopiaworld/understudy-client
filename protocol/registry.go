package protocol

import (
	"fmt"
	"slices"
	"sync"
)

// The version registry. Generated tables call Register from their init, so
// importing this package is enough to make every supported version available.
//
// It is a package-level registry rather than an injected one because the set
// of protocol versions is a property of the build, not of any one client: two
// Clients speaking different versions still agree on what "26.1" means.
var (
	registryMu sync.RWMutex
	byName     = map[string]*Version{}
	byProtocol = map[int32]*Version{}
)

// Register adds a version to the registry. It panics on a duplicate name or
// protocol number, which can only be a build-time mistake in the generated
// tables — two versions claiming one protocol number would otherwise make
// auto-detection silently pick whichever registered last.
func Register(v *Version) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if prev, ok := byName[v.Name]; ok {
		panic(fmt.Sprintf("protocol: version %q registered twice (protocols %d and %d)",
			v.Name, prev.Protocol, v.Protocol))
	}
	if prev, ok := byProtocol[v.Protocol]; ok {
		panic(fmt.Sprintf("protocol: protocol %d registered by both %q and %q",
			v.Protocol, prev.Name, v.Name))
	}
	byName[v.Name] = v
	byProtocol[v.Protocol] = v
}

// ByName looks up a version by its Minecraft version string, e.g. "26.1".
func ByName(name string) (*Version, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if v, ok := byName[name]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("protocol: unsupported version %q (have %v)", name, namesLocked())
}

// ByProtocol looks up a version by its wire protocol number. This is what a
// server-list ping reports, so it is the entry point for auto-detection.
func ByProtocol(p int32) (*Version, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if v, ok := byProtocol[p]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("protocol: unsupported protocol %d (have %v)", p, namesLocked())
}

// Names lists the registered version names, sorted.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return namesLocked()
}

// namesLocked builds the sorted name list. The caller holds registryMu.
func namesLocked() []string {
	out := make([]string, 0, len(byName))
	for name := range byName {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}
