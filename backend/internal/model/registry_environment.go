package model

import "strings"

// IsRegistryCredentialEnvironmentID reserves private Settings credential scope
// identifiers, including malformed/case/whitespace aliases. Stores use it to
// apply exclusions before public pagination without a service dependency.
func IsRegistryCredentialEnvironmentID(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	return id == "_registries" || strings.HasPrefix(id, "registry_")
}

// IsRegistryCredentialEnvironment includes friendly-name aliases: KeepSave
// resolves environments by name, so checking only the ID is insufficient.
func IsRegistryCredentialEnvironment(env *Environment) bool {
	if env == nil {
		return false
	}
	return IsRegistryCredentialEnvironmentID(env.ID) || IsRegistryCredentialEnvironmentID(env.Name) || strings.ToLower(strings.TrimSpace(env.Name)) == "_registries (internal)"
}
