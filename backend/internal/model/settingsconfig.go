package model

import "time"

// RegistryConfig is a saved image-registry connection the operator
// configures from the Settings page (Docker Hub, GHCR, ECR, GCR…).
// Cooker authenticates against it on push/pull.
//
// The auth password/token is sensitive and never lives on this
// struct in plaintext after persistence: the service layer writes
// the bytes to secrets.Manager and stamps only PasswordRef here.
// Mirrors the model.Host SSHPrivateKeyRef pattern; build a
// client-safe view with Redact.
type RegistryConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`

	// PasswordRef names a secrets.Manager URI holding the registry
	// password/token. The secret bytes never travel on this struct —
	// only the reference does. Never serialised; redacted with the
	// HasPassword flag instead.
	PasswordRef string `json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RegistryConfigResponse is the wire shape returned to clients. It
// excludes the secrets-manager reference for the password but exposes
// a derived HasPassword flag the UI can use to render "credential on
// file" vs "anonymous" states. Build with RegistryConfig.Redact.
type RegistryConfigResponse struct {
	*RegistryConfig
	HasPassword bool `json:"hasPassword"`
}

// Redact returns a RegistryConfigResponse safe for client responses.
// PasswordRef is dropped (it is already json:"-" so this is
// belt-and-suspenders); only the existence flag travels.
func (r *RegistryConfig) Redact() *RegistryConfigResponse {
	cp := *r
	cp.PasswordRef = ""
	return &RegistryConfigResponse{RegistryConfig: &cp, HasPassword: r.PasswordRef != ""}
}

// ClusterConfig is a saved Kubernetes cluster connection the operator
// configures from the Settings page. Cooker can deploy into multiple
// clusters via different kubeconfigs/contexts.
//
// The kubeconfig body is sensitive (it carries cluster credentials)
// and is handled exactly like RegistryConfig's password: the service
// layer writes it to secrets.Manager and stamps only KubeconfigRef
// here. Build a client-safe view with Redact.
type ClusterConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Context string `json:"context,omitempty"`

	// KubeconfigRef names a secrets.Manager URI holding the kubeconfig
	// body. The secret bytes never travel on this struct — only the
	// reference does. Never serialised; redacted with the
	// HasCredentials flag instead.
	KubeconfigRef string `json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ClusterConfigResponse is the wire shape returned to clients. It
// excludes the secrets-manager reference for the kubeconfig but
// exposes a derived HasCredentials flag the UI can use to render
// "kubeconfig on file" state. Build with ClusterConfig.Redact.
type ClusterConfigResponse struct {
	*ClusterConfig
	HasCredentials bool `json:"hasCredentials"`
}

// Redact returns a ClusterConfigResponse safe for client responses.
// KubeconfigRef is dropped (already json:"-"); only the existence
// flag travels.
func (c *ClusterConfig) Redact() *ClusterConfigResponse {
	cp := *c
	cp.KubeconfigRef = ""
	return &ClusterConfigResponse{ClusterConfig: &cp, HasCredentials: c.KubeconfigRef != ""}
}
