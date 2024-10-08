package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/jukie/karpenter-deprovision-controller/pkg/metrics"

	"github.com/go-openapi/jsonpointer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/robfig/cron/v3"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	DisruptionWindowSchedKey    = "k8s.jukie.net/disruption-window-schedule"
	DisruptionWindowDurationKey = "k8s.jukie.net/disruption-window-duration"
)

type NodeClaimController struct {
	Client client.Client
}

func (c *NodeClaimController) Reconcile(ctx context.Context, nClaim *karpv1.NodeClaim) (reconcile.Result, error) {
	// Get pods from expired NodeClaim
	var podList corev1.PodList
	if err := c.Client.List(ctx, &podList, client.MatchingFields{"spec.nodeName": nClaim.Status.NodeName}); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed getting pods from cache: %w", err)
	}

	// Handle the blocking pods.
	c.HandleBlockingPods(ctx, podList.Items, nClaim.Status.NodeName)
	return reconcile.Result{}, nil
}

func (c *NodeClaimController) Register(_ context.Context, mgr manager.Manager) error {
	return ctrlruntime.NewControllerManagedBy(mgr).
		Named("nodeclaim-nodeClaimController").
		For(&karpv1.NodeClaim{}, builder.WithPredicates(predicate.Funcs{
			// Reconcile all expired nodes upon startup
			CreateFunc: func(e event.CreateEvent) bool {
				nClaim := e.Object.(*karpv1.NodeClaim)
				return nClaim.StatusConditions().IsTrue("Expired")
			},
			// Reconcile only when the NodeClaim becomes expired.
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldObj := e.ObjectOld.(*karpv1.NodeClaim)
				newObj := e.ObjectNew.(*karpv1.NodeClaim)
				return !oldObj.StatusConditions().IsTrue("Expired") && newObj.StatusConditions().IsTrue("Expired")
			},
			// Skip delete and generic events
			DeleteFunc:  func(e event.DeleteEvent) bool { return false },
			GenericFunc: func(e event.GenericEvent) bool { return false },
		})).
		Complete(reconcile.AsReconciler(mgr.GetClient(), c))
}

func (c *NodeClaimController) HandleBlockingPods(ctx context.Context, pods []corev1.Pod, nodeName string) {
	// Loop over pods on expired Node and conditionally remove blocking annotations
	for _, pod := range pods {
		if pod.Annotations[karpv1.DoNotDisruptAnnotationKey] == "" {
			continue
		}
		// Check if configured Disruption Window is active
		if !IsDisruptionWindowActive(ctx, pod.Namespace, pod.Name, pod.Annotations[DisruptionWindowSchedKey], pod.Annotations[DisruptionWindowDurationKey]) {
			continue
		}

		log.FromContext(ctx).Info(fmt.Sprintf("Node %s has exceeded its max lifetime and will now remove do-not-disrupt annotations from the following pod to allow for deprovisioning: namespace: %s, pod name: %s", nodeName, pod.Namespace, pod.Name))
		patch := fmt.Sprintf(`[{"op":"remove", "path":"/metadata/annotations/%s"}]`, jsonpointer.Escape(karpv1.DoNotDisruptAnnotationKey))
		rawPatch := client.RawPatch(types.JSONPatchType, []byte(patch))
		if err := c.Client.Patch(ctx, &pod, rawPatch); err != nil {
			log.FromContext(ctx).Error(err, fmt.Sprintf("Failed to remove annotations from pod %s/%s", pod.Namespace, pod.Name))
			metrics.PatchCounter.With(prometheus.Labels{
				metrics.KindLabel:      pod.Kind,
				metrics.NameLabel:      pod.Name,
				metrics.SucceededLabel: "false",
			}).Inc()
			continue
		}
		log.FromContext(ctx).Info(fmt.Sprintf("Annotation %s removed from pod %s in namespace %s", karpv1.DoNotDisruptAnnotationKey, pod.Name, pod.Namespace))
	}
}

// IsDisruptionWindowActive checks if the current time is within the disruption window.
func IsDisruptionWindowActive(ctx context.Context, podNamespace, podName string, disruptionWindowSched, disruptionWindowDuration string) bool {
	pod := podNamespace + "/" + podName
	if disruptionWindowSched == "" {
		return true
	}
	schedule, err := cron.ParseStandard(fmt.Sprintf("TZ=UTC %s", disruptionWindowSched))
	if err != nil {
		log.FromContext(ctx).Error(err, fmt.Sprintf("Failed to parse disruption window schedule for pod %s/%s", podNamespace, pod))
		metrics.FailedAnnotationParseCounter.With(prometheus.Labels{
			metrics.AnnotationType: "DisruptionWindowSchedule",
			metrics.NameLabel:      pod,
		}).Inc()
		return true
	}

	duration := 3 * time.Hour
	if disruptionWindowDuration != "" {
		if parsedDuration, err := time.ParseDuration(disruptionWindowDuration); err != nil || parsedDuration < 3*time.Hour {
			log.FromContext(ctx).Error(err, fmt.Sprintf("Invalid or too short disruption window duration for %s, using default of 3 hours", pod))
			metrics.FailedAnnotationParseCounter.With(prometheus.Labels{
				metrics.AnnotationType: "DisruptionWindowDuration",
				metrics.NameLabel:      pod,
			}).Inc()
		} else {
			duration = parsedDuration
		}
	}

	// Walk back in time for the duration associated with the schedule and check if current time is inside window
	now := time.Now().UTC()
	checkPoint := now.Add(-duration)
	nextHit := schedule.Next(checkPoint)
	return !nextHit.After(now)
}
