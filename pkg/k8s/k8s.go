// Package k8s provides Kubernetes chaos experiments: killing pods, scaling down
// deployments, and deleting services.
package k8s

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
	"yacmo/pkg/safety"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ChaosK8s implements chaos.Experiment for Kubernetes resources.
type ChaosK8s struct {
	cfg       config.KubernetesConfig
	policy    *safety.Policy
	log       *logger.Logger
	clientset kubernetes.Interface
	// Track what we affected for rollback
	killedPods     []podRef
	scaledDownDeps []depRef
}

type podRef struct {
	Namespace string
	Name      string
}

type depRef struct {
	Namespace    string
	Name         string
	OrigReplicas int32
}

// New creates a new Kubernetes chaos experiment.
func New(cfg config.KubernetesConfig, policy *safety.Policy, log *logger.Logger) (*ChaosK8s, error) {
	var restCfg *rest.Config
	var err error

	if cfg.Kubeconfig != "" {
		restCfg, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	} else {
		restCfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("building k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("creating k8s clientset: %w", err)
	}

	return &ChaosK8s{
		cfg:       cfg,
		policy:    policy,
		log:       log,
		clientset: clientset,
	}, nil
}

// NewWithClient creates a ChaosK8s with an injected Kubernetes client (useful for testing).
func NewWithClient(cfg config.KubernetesConfig, log *logger.Logger, client kubernetes.Interface) *ChaosK8s {
	return &ChaosK8s{
		cfg:       cfg,
		log:       log,
		clientset: client,
	}
}

// Name returns the experiment name.
func (c *ChaosK8s) Name() string {
	return fmt.Sprintf("k8s-chaos[namespaces=%s, actions=%s]",
		strings.Join(c.cfg.Namespaces, ","),
		strings.Join(c.cfg.Actions, ","))
}

// Run executes the Kubernetes chaos experiment.
func (c *ChaosK8s) Run(ctx context.Context) error {
	for _, action := range c.cfg.Actions {
		switch action {
		case "kill_pod":
			if err := c.killPods(ctx); err != nil {
				return fmt.Errorf("kill_pod: %w", err)
			}
		case "scale_down":
			if err := c.scaleDownDeployments(ctx); err != nil {
				return fmt.Errorf("scale_down: %w", err)
			}
		case "delete_service":
			if err := c.deleteServices(ctx); err != nil {
				return fmt.Errorf("delete_service: %w", err)
			}
		default:
			c.log.Warn("Unknown k8s action: %s", action)
		}
	}
	return nil
}

// DestructiveActionCount returns the number of destructive Kubernetes actions configured.
func (c *ChaosK8s) DestructiveActionCount() int {
	total := 0
	for _, action := range c.cfg.Actions {
		switch action {
		case "kill_pod", "scale_down", "delete_service":
			total += len(c.cfg.Namespaces)
		}
	}
	return total
}

// Rollback attempts to undo the chaos (best-effort).
func (c *ChaosK8s) Rollback(ctx context.Context) error {
	var errs []string

	// Restore scaled-down deployments
	for _, dep := range c.scaledDownDeps {
		c.log.Info("Restoring deployment %s/%s to %d replicas", dep.Namespace, dep.Name, dep.OrigReplicas)
		scale, err := c.clientset.AppsV1().Deployments(dep.Namespace).GetScale(ctx, dep.Name, metav1.GetOptions{})
		if err != nil {
			errs = append(errs, fmt.Sprintf("get scale %s/%s: %v", dep.Namespace, dep.Name, err))
			continue
		}
		scale.Spec.Replicas = dep.OrigReplicas
		_, err = c.clientset.AppsV1().Deployments(dep.Namespace).UpdateScale(ctx, dep.Name, scale, metav1.UpdateOptions{})
		if err != nil {
			errs = append(errs, fmt.Sprintf("update scale %s/%s: %v", dep.Namespace, dep.Name, err))
		}
	}

	// Note: killed pods cannot be "un-killed" — Kubernetes should reschedule them
	// if they are managed by a controller (Deployment, ReplicaSet, etc.)
	if len(c.killedPods) > 0 {
		c.log.Info("Killed %d pods — they should be rescheduled by their controllers", len(c.killedPods))
	}

	if len(errs) > 0 {
		return fmt.Errorf("rollback errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// killPods randomly kills pods in the target namespaces.
func (c *ChaosK8s) killPods(ctx context.Context) error {
	for _, ns := range c.cfg.Namespaces {
		listOpts := metav1.ListOptions{}
		if len(c.cfg.LabelSelectors) > 0 {
			listOpts.LabelSelector = strings.Join(c.cfg.LabelSelectors, ",")
		}

		pods, err := c.clientset.CoreV1().Pods(ns).List(ctx, listOpts)
		if err != nil {
			return fmt.Errorf("listing pods in %s: %w", ns, err)
		}

		if len(pods.Items) == 0 {
			c.log.Info("No pods found in namespace %s", ns)
			continue
		}

		// Filter out excluded pods
		var eligible []string
		for _, pod := range pods.Items {
			if c.isExcluded(pod.Name) {
				c.log.Debug("Excluding pod %s", pod.Name)
				continue
			}
			eligible = append(eligible, pod.Name)
		}

		if len(eligible) == 0 {
			c.log.Info("No eligible pods in namespace %s after exclusions", ns)
			continue
		}

		// Randomly select targets
		targets := selectRandom(eligible, c.cfg.MaxTargets)
		for _, podName := range targets {
			if c.policy != nil {
				if err := c.policy.CheckK8sTarget(ns, podName, "kill_pod"); err != nil {
					c.log.Error("Skipping pod %s/%s: %v", ns, podName, err)
					continue
				}
			}
			c.log.Info("🔪 Killing pod %s/%s", ns, podName)
			gracePeriod := c.cfg.GracePeriodSeconds
			err := c.clientset.CoreV1().Pods(ns).Delete(ctx, podName, metav1.DeleteOptions{
				GracePeriodSeconds: &gracePeriod,
			})
			if err != nil {
				c.log.Error("Failed to kill pod %s/%s: %v", ns, podName, err)
				continue
			}
			c.killedPods = append(c.killedPods, podRef{Namespace: ns, Name: podName})
			c.log.Info("✓ Pod %s/%s killed", ns, podName)
		}
	}
	return nil
}

// scaleDownDeployments scales deployments to 0 replicas.
func (c *ChaosK8s) scaleDownDeployments(ctx context.Context) error {
	for _, ns := range c.cfg.Namespaces {
		listOpts := metav1.ListOptions{}
		if len(c.cfg.LabelSelectors) > 0 {
			listOpts.LabelSelector = strings.Join(c.cfg.LabelSelectors, ",")
		}

		deployments, err := c.clientset.AppsV1().Deployments(ns).List(ctx, listOpts)
		if err != nil {
			return fmt.Errorf("listing deployments in %s: %w", ns, err)
		}

		if len(deployments.Items) == 0 {
			c.log.Info("No deployments found in namespace %s", ns)
			continue
		}

		var names []string
		origReplicas := make(map[string]int32)
		for _, dep := range deployments.Items {
			if c.isExcluded(dep.Name) {
				continue
			}
			names = append(names, dep.Name)
			if dep.Spec.Replicas != nil {
				origReplicas[dep.Name] = *dep.Spec.Replicas
			} else {
				origReplicas[dep.Name] = 1
			}
		}

		targets := selectRandom(names, c.cfg.MaxTargets)
		for _, depName := range targets {
			if c.policy != nil {
				if err := c.policy.CheckK8sTarget(ns, depName, "scale_down"); err != nil {
					c.log.Error("Skipping deployment %s/%s: %v", ns, depName, err)
					continue
				}
			}
			c.log.Info("📉 Scaling down deployment %s/%s from %d to 0", ns, depName, origReplicas[depName])

			scale, err := c.clientset.AppsV1().Deployments(ns).GetScale(ctx, depName, metav1.GetOptions{})
			if err != nil {
				c.log.Error("Failed to get scale for %s/%s: %v", ns, depName, err)
				continue
			}

			c.scaledDownDeps = append(c.scaledDownDeps, depRef{
				Namespace:    ns,
				Name:         depName,
				OrigReplicas: origReplicas[depName],
			})

			scale.Spec.Replicas = 0
			_, err = c.clientset.AppsV1().Deployments(ns).UpdateScale(ctx, depName, scale, metav1.UpdateOptions{})
			if err != nil {
				c.log.Error("Failed to scale down %s/%s: %v", ns, depName, err)
				continue
			}
			c.log.Info("✓ Deployment %s/%s scaled to 0", ns, depName)
		}
	}
	return nil
}

// deleteServices randomly deletes services in the target namespaces.
func (c *ChaosK8s) deleteServices(ctx context.Context) error {
	for _, ns := range c.cfg.Namespaces {
		listOpts := metav1.ListOptions{}
		if len(c.cfg.LabelSelectors) > 0 {
			listOpts.LabelSelector = strings.Join(c.cfg.LabelSelectors, ",")
		}

		services, err := c.clientset.CoreV1().Services(ns).List(ctx, listOpts)
		if err != nil {
			return fmt.Errorf("listing services in %s: %w", ns, err)
		}

		if len(services.Items) == 0 {
			c.log.Info("No services found in namespace %s", ns)
			continue
		}

		var names []string
		for _, svc := range services.Items {
			// Never delete the "kubernetes" service
			if svc.Name == "kubernetes" {
				continue
			}
			if c.isExcluded(svc.Name) {
				continue
			}
			names = append(names, svc.Name)
		}

		targets := selectRandom(names, c.cfg.MaxTargets)
		for _, svcName := range targets {
			if c.policy != nil {
				if err := c.policy.CheckK8sTarget(ns, svcName, "delete_service"); err != nil {
					c.log.Error("Skipping service %s/%s: %v", ns, svcName, err)
					continue
				}
			}
			c.log.Info("🗑️  Deleting service %s/%s", ns, svcName)
			err := c.clientset.CoreV1().Services(ns).Delete(ctx, svcName, metav1.DeleteOptions{})
			if err != nil {
				c.log.Error("Failed to delete service %s/%s: %v", ns, svcName, err)
				continue
			}
			c.log.Info("✓ Service %s/%s deleted", ns, svcName)
		}
	}
	return nil
}

// isExcluded checks if a resource name matches any exclusion pattern.
func (c *ChaosK8s) isExcluded(name string) bool {
	for _, pattern := range c.cfg.ExcludedPods {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}

// selectRandom picks up to max random items from the slice.
func selectRandom(items []string, max int) []string {
	if max <= 0 || max >= len(items) {
		return items
	}
	rand.Shuffle(len(items), func(i, j int) {
		items[i], items[j] = items[j], items[i]
	})
	return items[:max]
}
