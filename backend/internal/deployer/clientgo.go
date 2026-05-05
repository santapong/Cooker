package deployer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	memcache "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// ClientGo applies manifests via the Kubernetes dynamic client. The
// dynamic client is the natural fit because Cooker pipelines hand
// over arbitrary YAML — the kinds aren't known statically.
//
// Helm and Compose request kinds still return ErrUnavailable; only
// raw-manifest Kind is wired here. The Helm path lands separately
// (roadmap P9.2 / P9.5 wave).
type ClientGo struct {
	Kubeconfig string // empty means in-cluster

	// dynamic, mapper, and discovery are lazy-initialised on first
	// Deploy so constructing a ClientGo doesn't require cluster auth.
	// initOnce serialises lazy init across concurrent Deploy calls;
	// initErr captures the result of the one-shot init for callers.
	initOnce sync.Once
	initErr  error
	cli      dynamic.Interface
	mapper   *restmapper.DeferredDiscoveryRESTMapper
}

func NewClientGo(kubeconfig string) *ClientGo { return &ClientGo{Kubeconfig: kubeconfig} }

func (c *ClientGo) restConfig() (*rest.Config, error) {
	if c.Kubeconfig == "" {
		cfg, err := rest.InClusterConfig()
		if err == nil {
			return cfg, nil
		}
		// Fall through to default loading rules — works on a workstation.
	}
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	if c.Kubeconfig != "" {
		loader.ExplicitPath = c.Kubeconfig
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{}).ClientConfig()
}

func (c *ClientGo) ensureClients() error {
	c.initOnce.Do(func() {
		cfg, err := c.restConfig()
		if err != nil {
			c.initErr = fmt.Errorf("%w: rest config: %v", ErrUnavailable, err)
			return
		}
		dyn, err := dynamic.NewForConfig(cfg)
		if err != nil {
			c.initErr = fmt.Errorf("%w: dynamic client: %v", ErrUnavailable, err)
			return
		}
		disc, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			c.initErr = fmt.Errorf("%w: discovery: %v", ErrUnavailable, err)
			return
		}
		c.cli = dyn
		c.mapper = restmapper.NewDeferredDiscoveryRESTMapper(memcache.NewMemCacheClient(disc))
	})
	return c.initErr
}

// Deploy applies the manifest in req. Server-side apply is used so
// conflicts surface as proper errors rather than silent overwrites.
func (c *ClientGo) Deploy(ctx context.Context, req Request) (Result, error) {
	if req.Kind != KindManifest {
		return Result{}, fmt.Errorf("%w: client-go deployer only supports KindManifest (got %q)", ErrUnavailable, req.Kind)
	}
	if err := c.ensureClients(); err != nil {
		return Result{}, err
	}
	docs, err := splitManifest(req.Manifest)
	if err != nil {
		return Result{}, fmt.Errorf("%w: split manifest: %v", ErrUnavailable, err)
	}
	var applied []string
	for _, raw := range docs {
		obj := &unstructured.Unstructured{}
		if _, _, err := unstructuredDecoder.Decode(raw, nil, obj); err != nil {
			return Result{}, fmt.Errorf("%w: decode: %v", ErrUnavailable, err)
		}
		ns := req.Namespace
		if obj.GetNamespace() != "" {
			ns = obj.GetNamespace()
		}
		gvk := obj.GroupVersionKind()
		mapping, err := c.mapper.RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
		if err != nil {
			return Result{}, fmt.Errorf("%w: rest mapping for %s: %v", ErrUnavailable, gvk, err)
		}
		var resourceCli dynamic.ResourceInterface
		if mapping.Scope.Name() == "namespace" {
			resourceCli = c.cli.Resource(mapping.Resource).Namespace(ns)
		} else {
			resourceCli = c.cli.Resource(mapping.Resource)
		}
		// Server-Side Apply with Cooker as the field manager.
		data, err := obj.MarshalJSON()
		if err != nil {
			return Result{}, fmt.Errorf("%w: marshal: %v", ErrUnavailable, err)
		}
		out, err := resourceCli.Patch(ctx, obj.GetName(), apiTypesApplyPatchType, data, metav1.PatchOptions{
			FieldManager: "cooker",
			Force:        boolPtr(true),
		})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return Result{}, fmt.Errorf("%w: apply %s: %v", ErrUnavailable, gvk, err)
		}
		applied = append(applied, fmt.Sprintf("%s/%s/%s", out.GetKind(), out.GetNamespace(), out.GetName()))
	}
	return Result{AppliedResources: applied}, nil
}

// maxManifestBytes caps the total size of an inbound manifest
// payload. K8s itself accepts up to a couple of MB per object;
// 4 MiB across the entire multi-doc YAML is more than enough for
// realistic apps and keeps "billion laughs" / nesting-bomb attacks
// from hanging the parser.
const maxManifestBytes = 4 << 20

// maxManifestDocs caps the number of documents in a single payload.
// A pathological YAML can include thousands of trivial documents to
// inflate parse time even within the byte cap.
const maxManifestDocs = 64

// errManifestTooLarge is the error returned when a manifest exceeds
// either the byte or document cap.
var errManifestTooLarge = errors.New("deployer: manifest exceeds size or document limit")

func splitManifest(b []byte) ([][]byte, error) {
	if len(b) > maxManifestBytes {
		return nil, errManifestTooLarge
	}
	r := utilyaml.NewYAMLReader(yamlBufReader(b))
	var docs [][]byte
	for {
		raw, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		docs = append(docs, raw)
		if len(docs) > maxManifestDocs {
			return nil, errManifestTooLarge
		}
	}
	return docs, nil
}

func boolPtr(b bool) *bool { return &b }

var _ Deployer = (*ClientGo)(nil)
