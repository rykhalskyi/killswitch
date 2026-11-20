package flux

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	GroupKustomize = "kustomize.toolkit.fluxcd.io"
	GroupHelm      = "helm.toolkit.fluxcd.io"

	KindKustomization = "Kustomization"
	KindHelmRelease   = "HelmRelease"
)

// ResourceInfo represents the live state of a Flux custom resource.
type ResourceInfo struct {
	Kind               string                     `json:"kind"`
	Namespace          string                     `json:"namespace"`
	Name               string                     `json:"name"`
	Suspended          bool                       `json:"suspended"`
	Generation         int64                      `json:"generation"`
	ObservedGeneration int64                      `json:"observedGeneration"`
	Ready              string                     `json:"ready"` // "True", "False", "Unknown"
	ReadyReason        string                     `json:"readyReason"`
	ReadyMessage       string                     `json:"readyMessage"`
	Raw                *unstructured.Unstructured `json:"-"`
}

// Client defines the interface for interacting with Flux resources on Kubernetes.
type Client interface {
	DiscoverGVRs(ctx context.Context) error
	GetGVR(kind string) (schema.GroupVersionResource, error)
	GetResource(ctx context.Context, kind, namespace, name string) (*ResourceInfo, error)
	PatchSuspend(ctx context.Context, kind, namespace, name string, suspend bool) (*ResourceInfo, error)
}

// KubeFluxClient implements Client using dynamic.Interface and discovery.DiscoveryInterface.
type KubeFluxClient struct {
	dynamicClient   dynamic.Interface
	discoveryClient discovery.DiscoveryInterface
	gvrs            map[string]schema.GroupVersionResource
}

// NewClient creates a new KubeFluxClient from kubeconfig path and optional context.
func NewClient(kubeconfigPath, contextName string) (*KubeFluxClient, string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		configOverrides.CurrentContext = contextName
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return nil, "", fmt.Errorf("loading kubeconfig: %w", err)
	}

	activeContext := rawConfig.CurrentContext
	if contextName != "" {
		activeContext = contextName
	}

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, "", fmt.Errorf("building kubernetes client config: %w", err)
	}

	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, "", fmt.Errorf("creating dynamic kubernetes client: %w", err)
	}

	disco, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, "", fmt.Errorf("creating discovery kubernetes client: %w", err)
	}

	return NewClientFromClients(dyn, disco), activeContext, nil
}

// NewClientFromConfig creates a client using an existing *rest.Config.
func NewClientFromConfig(restConfig *rest.Config) (*KubeFluxClient, error) {
	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic kubernetes client: %w", err)
	}

	disco, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating discovery kubernetes client: %w", err)
	}

	return NewClientFromClients(dyn, disco), nil
}

// NewClientFromClients initializes a KubeFluxClient with dynamic and discovery instances.
func NewClientFromClients(dyn dynamic.Interface, disco discovery.DiscoveryInterface) *KubeFluxClient {
	return &KubeFluxClient{
		dynamicClient:   dyn,
		discoveryClient: disco,
		gvrs: map[string]schema.GroupVersionResource{
			KindKustomization: {Group: GroupKustomize, Version: "v1", Resource: "kustomizations"},
			KindHelmRelease:   {Group: GroupHelm, Version: "v2", Resource: "helmreleases"},
		},
	}
}

// DiscoverGVRs uses the Kubernetes discovery API to discover the exact GVR for Kustomization and HelmRelease.
func (c *KubeFluxClient) DiscoverGVRs(ctx context.Context) error {
	if c.discoveryClient == nil {
		return nil
	}

	resourceList, err := c.discoveryClient.ServerPreferredResources()
	if err != nil && len(resourceList) == 0 {
		return fmt.Errorf("discovering api resources: %w", err)
	}

	for _, groupList := range resourceList {
		gv, err := schema.ParseGroupVersion(groupList.GroupVersion)
		if err != nil {
			continue
		}

		for _, apiRes := range groupList.APIResources {
			if gv.Group == GroupKustomize && apiRes.Kind == KindKustomization {
				c.gvrs[KindKustomization] = schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: apiRes.Name,
				}
			}
			if gv.Group == GroupHelm && apiRes.Kind == KindHelmRelease {
				c.gvrs[KindHelmRelease] = schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: apiRes.Name,
				}
			}
		}
	}

	return nil
}

// Dynamic returns the underlying dynamic client for other action handlers.
func (c *KubeFluxClient) Dynamic() dynamic.Interface {
	return c.dynamicClient
}

// Discovery returns the underlying discovery client.
func (c *KubeFluxClient) Discovery() discovery.DiscoveryInterface {
	return c.discoveryClient
}

// GetGVR returns the discovered or default GVR for a kind.
func (c *KubeFluxClient) GetGVR(kind string) (schema.GroupVersionResource, error) {
	gvr, ok := c.gvrs[kind]
	if !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("unknown kind %q", kind)
	}
	return gvr, nil
}

// GetResource fetches a single Flux custom resource and extracts status and spec.suspend.
func (c *KubeFluxClient) GetResource(ctx context.Context, kind, namespace, name string) (*ResourceInfo, error) {
	gvr, err := c.GetGVR(kind)
	if err != nil {
		return nil, err
	}

	obj, err := c.dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return ExtractResourceInfo(obj, kind, namespace, name), nil
}

// PatchSuspend patches spec.suspend on the resource and immediately reads back the live state.
func (c *KubeFluxClient) PatchSuspend(ctx context.Context, kind, namespace, name string, suspend bool) (*ResourceInfo, error) {
	gvr, err := c.GetGVR(kind)
	if err != nil {
		return nil, err
	}

	patch := fmt.Sprintf(`{"spec":{"suspend":%t}}`, suspend)
	_, err = c.dynamicClient.Resource(gvr).Namespace(namespace).Patch(
		ctx,
		name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("patching %s/%s/%s: %w", kind, namespace, name, err)
	}

	// Read-back verification
	return c.GetResource(ctx, kind, namespace, name)
}

// ExtractResourceInfo parses unstructured Kubernetes object data into ResourceInfo.
func ExtractResourceInfo(obj *unstructured.Unstructured, kind, namespace, name string) *ResourceInfo {
	info := &ResourceInfo{
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
		Generation: obj.GetGeneration(),
		Ready:      "Unknown",
		Raw:        obj,
	}

	// Extract spec.suspend
	if suspend, found, err := unstructured.NestedBool(obj.Object, "spec", "suspend"); err == nil && found {
		info.Suspended = suspend
	} else {
		info.Suspended = false
	}

	// Extract status.observedGeneration
	if obsGen, found, err := unstructured.NestedInt64(obj.Object, "status", "observedGeneration"); err == nil && found {
		info.ObservedGeneration = obsGen
	}

	// Extract Ready condition
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err == nil && found {
		for _, rawCond := range conditions {
			cond, ok := rawCond.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _ := cond["type"].(string)
			if strings.EqualFold(condType, "Ready") {
				if status, ok := cond["status"].(string); ok {
					info.Ready = status
				}
				if reason, ok := cond["reason"].(string); ok {
					info.ReadyReason = reason
				}
				if msg, ok := cond["message"].(string); ok {
					info.ReadyMessage = msg
				}
				break
			}
		}
	}

	return info
}
