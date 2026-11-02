package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

// Every stage gets a private overlay, leaving the reviewed base file immutable
// while parallel services deploy different build outputs. JSON is valid YAML.
func composeImageOverride(service, image string, runIDs ...string) (string, func(), error) {
	noop := func() {}
	runID := ""
	if len(runIDs) > 0 {
		runID = runIDs[0]
	}
	if !strings.Contains(image, "@") && runID == "" {
		return "", noop, nil
	}
	if strings.Contains(image, "@") {
		if _, err := name.NewDigest(image); err != nil {
			return "", noop, fmt.Errorf("invalid immutable Compose image")
		}
	}
	serviceConfig := map[string]any{}
	if strings.Contains(image, "@") {
		serviceConfig["image"] = image
	}
	if runID != "" {
		serviceConfig["labels"] = map[string]string{RuntimeRunLabel: runID}
	}
	data, err := json.Marshal(map[string]any{"services": map[string]any{service: serviceConfig}})
	if err != nil {
		return "", noop, err
	}
	f, err := os.CreateTemp("", "cooker-compose-image-*.json")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return "", noop, err
	}
	if err = f.Close(); err != nil {
		cleanup()
		return "", noop, err
	}
	return f.Name(), cleanup, nil
}
