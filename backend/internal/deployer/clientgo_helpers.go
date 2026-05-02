package deployer

import (
	"bufio"
	"bytes"

	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
)

// apiTypesApplyPatchType is server-side apply's patch content type.
const apiTypesApplyPatchType = apitypes.ApplyPatchType

// unstructuredDecoder decodes any GVK into *unstructured.Unstructured;
// the dynamic client doesn't need typed schemes.
var unstructuredDecoder = scheme.Codecs.UniversalDeserializer()

// yamlBufReader wraps a byte slice as a *bufio.Reader for the
// YAMLReader API.
func yamlBufReader(b []byte) *bufio.Reader {
	return bufio.NewReader(bytes.NewReader(b))
}

// silence the compiler about the imported alias.
var _ = yaml.NewYAMLReader
