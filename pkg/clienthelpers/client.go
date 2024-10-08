package clienthelpers

import (
	"context"
	"fmt"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
