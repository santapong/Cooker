package model

import "time"

// HostKind identifies what a managed Host runs.
type HostKind string

const (
	HostKindDocker     HostKind = "docker"
	HostKindKubernetes HostKind = "kubernetes"
	// HostKindSSHDocker is a remote Docker host reachable only via
	// SSH — Cooker connects with a private key, runs `docker pull` /
	// `docker run` on the box, and never exposes a docker API socket
	// over the network. This is the Dokploy / Coolify deployment
	// model. See deploytarget/ssh.
	HostKindSSHDocker HostKind = "ssh-docker"
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

	// SSH fields are used when Kind==HostKindSSHDocker. SSHEndpoint
	// is the host:port form ("1.2.3.4:22"); SSHPort is the canonical
	// port (handler defaults it to 22 on Create when zero).
	// SSHPrivateKeyRef names a secrets.Manager URI holding the PEM
	// body — the key bytes never live on the Host struct, only the
	// reference does. SSHKnownHostKey is populated on first connect
	// (TOFU) and refuses on key change unless SSHStrictHostKey is
	// false. SSHStrictHostKey=false is rejected in production by
	// Config.Validate.
	SSHEndpoint      string `json:"sshEndpoint,omitempty"`
	SSHUser          string `json:"sshUser,omitempty"`
	SSHPort          int    `json:"sshPort,omitempty"`
	SSHPrivateKeyRef string `json:"-"` // never serialised; redacted with HasSSHPrivateKey instead
	SSHKnownHostKey  string `json:"sshKnownHostKey,omitempty"`
	SSHStrictHostKey bool   `json:"sshStrictHostKey"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Version powers optimistic concurrency on Update; see store.ErrConflict.
	Version int `json:"version"`
}

// HostResponse is the wire shape returned to clients. It excludes
// the raw secrets-manager reference for the SSH private key but
// exposes a derived HasSSHPrivateKey flag the UI can use to render
// "key on file" vs "no key" states. Build with Host.Redact.
type HostResponse struct {
	*Host
	HasSSHPrivateKey bool `json:"hasSSHPrivateKey"`
}

// Redact returns a HostResponse safe for client responses. The
// SSHPrivateKeyRef is dropped (it's already json:"-" so this is
// belt-and-suspenders); only the existence flag travels.
func (h *Host) Redact() *HostResponse {
	cp := *h
	cp.SSHPrivateKeyRef = ""
	return &HostResponse{Host: &cp, HasSSHPrivateKey: h.SSHPrivateKeyRef != ""}
}
