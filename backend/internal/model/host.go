package model

import "time"

// HostKind identifies what a managed Host runs.
type HostKind string

const (
	HostKindDocker     HostKind = "docker"
	HostKindKubernetes HostKind = "kubernetes"
)

// HostReachability selects how Cooker dials the host. Direct means
// a plain TCP/HTTPS endpoint; Tailnet means the host is only
// reachable over the Tailscale tailnet Cooker joins via tsnet.
type HostReachability string

const (
	HostDirect  HostReachability = "direct"
	HostTailnet HostReachability = "tailnet"
)

// Host is a managed runtime (Docker host or K8s cluster) that an
// App can target. The handler layer dials it through the transport
// selected by Reachability: direct net.Dial for HostDirect, the
// tsnet dialer for HostTailnet.
type Host struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Kind HostKind `json:"kind"`

	Reachability HostReachability `json:"reachability"`

	// DockerEndpoint is used when Kind==HostKindDocker (e.g.
	// "tcp://10.0.0.3:2375" for direct, or "tcp://myhost:2375" for
	// tailnet where myhost is a MagicDNS name).
	DockerEndpoint string `json:"dockerEndpoint,omitempty"`

	// KubeconfigRef is used when Kind==HostKindKubernetes. It names
	// a kubeconfig stored as a Secret (via the env codec) keyed by
	// this host's ID.
	KubeconfigRef string `json:"kubeconfigRef,omitempty"`

	// TailnetIP is populated by the tsnet transport after first
	// contact; empty for direct hosts.
	TailnetIP string `json:"tailnetIp,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Version powers optimistic concurrency on Update; see store.ErrConflict.
	Version int `json:"version"`
}
