package model

// DockerNetwork describes a docker network as returned by
// `docker network inspect`. Fields mirror the Docker Engine API
// subset the UI consumes.
type DockerNetwork struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Driver string            `json:"driver"` // bridge|overlay|host|macvlan|ipvlan
	Scope  string            `json:"scope"`  // local|swarm|global
	Labels map[string]string `json:"labels,omitempty"`
	// HostID names the managed host (model.Host) this network lives
	// on. Empty means the local daemon Cooker talks to by default.
	HostID string `json:"hostId,omitempty"`
}

// DockerVolume describes a docker volume.
type DockerVolume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels,omitempty"`
	HostID     string            `json:"hostId,omitempty"`
}
