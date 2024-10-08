package clienthelpers

import (
	"context"
	"fmt"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpapis "sigs.k8s.io/karpenter/pkg/apis"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// GetConfig attempts to create an in-cluster configuration and falls back to using KUBECONFIG from the environment if in-cluster config is not available.
func GetConfig() *rest.Config {
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Info("In cluster config wasn't detected, trying to build from KUBECONFIG")
		kubeconfig := os.Getenv("KUBECONFIG")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			klog.Fatalf("Error building kubeconfig: %s", err.Error())
		}
	}
	return config
}

// GetScheme configures a runtime schema that includes Karpenter CRDs
func GetScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	gv := schema.GroupVersion{Group: karpapis.Group, Version: "v1"}
	v1.AddToGroupVersion(scheme, gv)
	scheme.AddKnownTypes(gv, &karpv1.NodeClaim{}, &karpv1.NodeClaimList{})
	if err := corev1.AddToScheme(scheme); err != nil {
		klog.Fatalf("Failed to add corev1 to scheme: %v", err)
	}
	return scheme
}

// PodIdxFunc is used for listing pods by node
var PodIdxFunc client.IndexerFunc = func(o client.Object) []string {
	pod := o.(*corev1.Pod)
	return []string{pod.Spec.NodeName}
}

// NewCache sets up a client cache with a custom field indexer for pods
func NewCache(config *rest.Config, options cache.Options) (cache.Cache, error) {
	clientCache, err := cache.New(config, options)
	if err != nil {
		return nil, fmt.Errorf("Issue building clientCache: %v", err)
	}

	// Needed for listing pods by node
	if err = clientCache.IndexField(context.TODO(), &corev1.Pod{}, "spec.nodeName", PodIdxFunc); err != nil {
		return nil, fmt.Errorf("Issue building Pod indexer: %v", err)
	}
	return clientCache, nil
}
