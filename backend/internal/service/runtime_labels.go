package service

import (
	"bytes"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// RuntimeStageLabel is stamped by the executor after all generated labels.
// Together with the run label it disambiguates actual full-ID stage receipts.
const RuntimeStageLabel = "io.cooker.stage-id"

// Stamp generated workload metadata and pod templates, never selectors. A
// rollout gets a new pod template while its Service selector stays stable.
func stampRuntimeManifest(manifest []byte, runID string) ([]byte, error) {
	if runID == "" {
		return nil, fmt.Errorf("runtime workload requires a run identity")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(manifest))
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("invalid generated workload manifest")
		}
		if len(doc) == 0 {
			continue
		}
		switch doc["kind"] {
		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job":
			stampRuntimeMetadata(doc, runID)
			stampRuntimeMetadata(runtimeChild(runtimeChild(doc, "spec"), "template"), runID)
		case "Pod":
			stampRuntimeMetadata(doc, runID)
		}
		if err := encoder.Encode(doc); err != nil {
			return nil, err
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func runtimeChild(parent map[string]any, key string) map[string]any {
	child, ok := parent[key].(map[string]any)
	if !ok {
		child = map[string]any{}
		parent[key] = child
	}
	return child
}

func stampRuntimeMetadata(object map[string]any, runID string) {
	runtimeChild(runtimeChild(object, "metadata"), "labels")[RuntimeRunLabel] = runID
}
