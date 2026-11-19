// Package kube gives access to the Kubernetes API when running in a cluster.
package kube

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const namespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// InCluster reports whether the process runs inside a Kubernetes pod.
func InCluster() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// NewClient builds a client from the pod's service account.
func NewClient() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}

// Namespace returns configured, falling back to the pod's own namespace.
func Namespace(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	b, err := os.ReadFile(namespaceFile)
	if err != nil {
		return "", fmt.Errorf("cannot determine namespace (set POD_NAMESPACE): %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
