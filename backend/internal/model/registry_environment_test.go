package model

import "testing"

func TestRegistryEnvironmentReservedIDsAndNames(t *testing.T) {
	for _, env := range []*Environment{
		{ID: "_registries", Name: "renamed"},
		{ID: "registry_private", Name: "renamed"},
		{ID: "ordinary-id", Name: "registry_private"},
		{ID: "ordinary-id", Name: " _REGISTRIES (INTERNAL) "},
		{ID: "ordinary-id", Name: " REGISTRY_private "},
	} {
		if !IsRegistryCredentialEnvironment(env) {
			t.Fatal("private ID/name alias became public")
		}
	}
	if IsRegistryCredentialEnvironment(nil) || IsRegistryCredentialEnvironment(&Environment{ID: "ordinary", Name: "production"}) {
		t.Fatal("ordinary environment misclassified")
	}
}
