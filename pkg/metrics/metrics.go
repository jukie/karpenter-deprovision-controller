package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	Namespace      = "karpenter"
	Subsystem      = "disruption_controller"
	KindLabel      = "kind"
	NameLabel      = "name"
	AnnotationType = "type"
	SucceededLabel = "succeeded"
)

var (
	PatchCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: Subsystem,
			Name:      "patch_operations_total",
			Help:      "Number of patch events in total by Karpenter Disruption Controller. Labeled by resource kind, resource name, and success status.",
		},
		[]string{
			KindLabel,
			NameLabel,
			SucceededLabel,
		},
	)
	FailedAnnotationParseCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: Subsystem,
			Name:      "annotation_parse_failed",
			Help:      "Number of annotation parsing failures in total by Karpenter Disruption Controller. Labeled by annotation type and pod name.",
		},
		[]string{
			AnnotationType,
			NameLabel,
		},
	)
)

func Register() {
	ctrlmetrics.Registry.MustRegister(PatchCounter, FailedAnnotationParseCounter)
}
