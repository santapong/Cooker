package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestSecretMetadataKeysAndArtifactTypesCannotExportKnownValues(t *testing.T) {
	ctx := WithLogSecrets(context.Background(), []string{"known-canary-secret"})
	sr := &model.StageRun{Outputs: map[string]string{"prefix-known-canary-secret": "innocent", "public": "setting"}, Artifacts: []model.Artifact{{Type: "known-canary-secret", Ref: "safe"}}}
	if err := protectStageMetadata(ctx, sr); err == nil {
		t.Fatal("secret key/type accepted")
	}
	data, _ := json.Marshal(sr)
	if strings.Contains(string(data), "known-canary-secret") || sr.Outputs["public"] != "setting" {
		t.Fatal("public metadata still contains known secret")
	}
}
