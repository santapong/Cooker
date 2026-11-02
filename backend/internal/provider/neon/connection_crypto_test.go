package neon

import (
	"bytes"
	"context"
	"testing"

	"github.com/santapong/cooker/internal/crypto"
)

func TestConnectionActualCodecLegacyAndTaggedCredentialsRemainScoped(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := crypto.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	s := &ConnectionService{Store: NewMemoryConnections(), Codec: codec, API: &connectionAPIFixture{}}
	c, err := s.Create(context.Background(), CreateConnectionRequest{Name: "Real codec fixture", OrganizationID: "org-fixture", AccountMode: "unknown", APIKey: connectionFixtureKey, ProjectIDs: []string{"project-one"}, AcknowledgeKeyPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(first.Credential, []byte(connectionFixtureKey)) {
		t.Fatal("actual codec stored plaintext")
	}
	if _, err = s.Sync(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	s.Codec = codec.WithVersionedWrites()
	replacement := "synthetic-next-neon-credential"
	updated, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: c.Version, APIKey: &replacement, AcknowledgeKeyPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Credential, stored.Credential) || updated.Snapshot != nil {
		t.Fatal("new credential generation retained old authority")
	}
	s.Codec = codec
	if resolved, err := s.key(stored); err != nil || resolved != replacement {
		t.Fatal("current reader could not read tagged credential")
	}
	stored.OrganizationID = "org-other"
	if _, err = s.key(stored); err == nil {
		t.Fatal("actual codec envelope allowed organization substitution")
	}
}
