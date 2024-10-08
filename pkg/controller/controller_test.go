package controller_test

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/jukie/karpenter-deprovision-controller/pkg/controller"
	"k8s.io/apimachinery/pkg/types"
	"testing"
	"time"

	"github.com/jukie/karpenter-deprovision-controller/pkg/clienthelpers"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func setupTestPod(name, namespace string, nodeName string, annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{NodeName: nodeName},
	}
}

func TestDeprovisionController_Reconcile(t *testing.T) {
	noBlockingAnnotationPod := setupTestPod("no-blocking-annotation", "testing", "test-node", nil)
	blockingNoSchedulePod := setupTestPod("blocking-no-sched", "testing", "test-node", map[string]string{
		karpv1.DoNotDisruptAnnotationKey: "true",
	})
	blockingInactiveSchedulePod := setupTestPod("blocking-inactive-sched", "testing", "test-node", map[string]string{
		karpv1.DoNotDisruptAnnotationKey:       "true",
		controller.DisruptionWindowSchedKey:    fmt.Sprintf("0 %d * * *", time.Now().Add(-4*time.Hour).UTC().Hour()),
		controller.DisruptionWindowDurationKey: "4h",
	})
	blockingActiveSchedulePod := setupTestPod("blocking-active-sched", "testing", "test-node", map[string]string{
		karpv1.DoNotDisruptAnnotationKey:    "true",
		controller.DisruptionWindowSchedKey: "* * * * *",
	})
	blockingInvalidSchedulePod := setupTestPod("blocking-invalid-sched", "testing", "test-node", map[string]string{
		karpv1.DoNotDisruptAnnotationKey:       "true",
		controller.DisruptionWindowSchedKey:    "hello",
		controller.DisruptionWindowDurationKey: "4h",
	})
	blockingInvalidDurationPod := setupTestPod("blocking-invalid-duration", "testing", "test-node", map[string]string{
		karpv1.DoNotDisruptAnnotationKey:       "true",
		controller.DisruptionWindowSchedKey:    "* * * * *",
		controller.DisruptionWindowDurationKey: "1h",
	})

	disruptionBlockedEvent := &corev1.Event{
		InvolvedObject: corev1.ObjectReference{
			Name: "test-node",
			Kind: "Node",
		},
		LastTimestamp: metav1.Time{Time: time.Now().Add(-30 * time.Minute).UTC()},
		Message:       controller.DisruptionBlockedEventMessage,
		Reason:        controller.DisruptionBlockedEventReason,
	}

	deprovisionController := &controller.DeprovisionController{}
	tests := []struct {
		name                    string
		pod                     *corev1.Pod
		objList                 []runtime.Object
		expectAnnotationRemoved bool
	}{
		{
			name:                    "No blocking annotation",
			pod:                     noBlockingAnnotationPod,
			objList:                 []runtime.Object{noBlockingAnnotationPod},
			expectAnnotationRemoved: false,
		},
		{
			name:                    "Pod with blocking annotation and no schedule",
			pod:                     blockingNoSchedulePod,
			objList:                 []runtime.Object{blockingNoSchedulePod},
			expectAnnotationRemoved: true,
		},
		{
			name:                    "Pod with blocking annotation and inactive schedule",
			pod:                     blockingInactiveSchedulePod,
			objList:                 []runtime.Object{blockingInactiveSchedulePod},
			expectAnnotationRemoved: false,
		},
		{
			name:                    "Pod with blocking annotation and invalid schedule",
			pod:                     blockingInvalidSchedulePod,
			objList:                 []runtime.Object{blockingInvalidSchedulePod},
			expectAnnotationRemoved: true,
		},
		{
			name:                    "Pod with blocking annotation and active schedule but invalid duration",
			pod:                     blockingInvalidDurationPod,
			objList:                 []runtime.Object{blockingInvalidDurationPod},
			expectAnnotationRemoved: true,
		},
		{
			name:                    "Pod with blocking annotation and active schedule",
			pod:                     blockingActiveSchedulePod,
			objList:                 []runtime.Object{blockingActiveSchedulePod},
			expectAnnotationRemoved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			deprovisionController.Client = fake.NewClientBuilder().
				WithRuntimeObjects(tt.objList...).
				WithIndex(&corev1.Pod{}, "spec.nodeName", clienthelpers.PodIdxFunc).
				Build()

			_, err := deprovisionController.Reconcile(context.TODO(), disruptionBlockedEvent)
			assert.NoError(t, err)

			updatedPod := &corev1.Pod{}
			err = deprovisionController.Client.Get(context.TODO(), types.NamespacedName{Name: tt.pod.Name, Namespace: tt.pod.Namespace}, updatedPod)
			assert.NoError(t, err)

			if tt.expectAnnotationRemoved {
				assert.Equal(t, "", updatedPod.Annotations[karpv1.DoNotDisruptAnnotationKey], "Expected annotation to be removed")
			} else {
				assert.Equal(t, tt.pod.Annotations[karpv1.DoNotDisruptAnnotationKey], updatedPod.Annotations[karpv1.DoNotDisruptAnnotationKey], "Expected annotation to be unchanged")
			}
		})
	}
}

func TestHandleBlockingPods(t *testing.T) {
	expiredNodeName := "test-node"
	noBlockingAnnotationPod := setupTestPod("no-blocking-annotation", "testing", expiredNodeName, nil)
	blockingNoSchedulePod := setupTestPod("blocking-no-sched", "testing", expiredNodeName, map[string]string{karpv1.DoNotDisruptAnnotationKey: "true"})
	blockingInactiveSchedulePod := setupTestPod("blocking-inactive-sched", "testing", expiredNodeName, map[string]string{
		karpv1.DoNotDisruptAnnotationKey:       "true",
		controller.DisruptionWindowSchedKey:    fmt.Sprintf("%d %d * * *", time.Now().UTC().Minute(), time.Now().Add(-4*time.Hour).UTC().Hour()),
		controller.DisruptionWindowDurationKey: "4h",
	})
	blockingActiveSchedulePod := setupTestPod("blocking-active-sched", "testing", "test-node", map[string]string{
		karpv1.DoNotDisruptAnnotationKey:       "true",
		controller.DisruptionWindowSchedKey:    fmt.Sprintf("%d %d * * *", time.Now().UTC().Minute(), time.Now().UTC().Hour()),
		controller.DisruptionWindowDurationKey: "5h",
	})

	tests := []struct {
		name                    string
		pods                    []corev1.Pod
		objList                 []runtime.Object
		expectAnnotationRemoved bool
	}{
		{
			name:                    "No blocking annotation",
			pods:                    []corev1.Pod{*noBlockingAnnotationPod},
			objList:                 []runtime.Object{noBlockingAnnotationPod},
			expectAnnotationRemoved: false,
		},
		{
			name:                    "Blocking annotation without schedule",
			pods:                    []corev1.Pod{*blockingNoSchedulePod},
			objList:                 []runtime.Object{blockingNoSchedulePod},
			expectAnnotationRemoved: true,
		},
		{
			name:                    "Blocking annotation with inactive schedule",
			pods:                    []corev1.Pod{*blockingInactiveSchedulePod},
			objList:                 []runtime.Object{blockingInactiveSchedulePod},
			expectAnnotationRemoved: false,
		},
		{
			name:                    "Blocking annotation with active schedule",
			pods:                    []corev1.Pod{*blockingActiveSchedulePod},
			objList:                 []runtime.Object{blockingActiveSchedulePod},
			expectAnnotationRemoved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake client with the test pods

			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(tt.objList...).Build()

			deprovisionController := &controller.DeprovisionController{
				Client: fakeClient,
			}
			deprovisionController.HandleBlockingPods(context.TODO(), tt.pods, "test-node")

			for _, pod := range tt.pods {
				updatedPod := &corev1.Pod{}
				err := deprovisionController.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updatedPod)
				assert.NoError(t, err)

				if tt.expectAnnotationRemoved {
					assert.Equal(t, "", updatedPod.Annotations[karpv1.DoNotDisruptAnnotationKey], "Expected annotation to be removed")
				} else {
					assert.Equal(t, pod.Annotations[karpv1.DoNotDisruptAnnotationKey], updatedPod.Annotations[karpv1.DoNotDisruptAnnotationKey], "Expected annotation to be unchanged")
				}
			}
		})
	}
}

func TestIsDisruptionWindowActive(t *testing.T) {
	podName := "test-pod"
	podNamespace := "test-namespace"
	tests := []struct {
		name                     string
		disruptionWindowSched    string
		disruptionWindowDuration string
		want                     bool
	}{
		{
			name:                     "Active schedule, valid duration",
			disruptionWindowSched:    "* * * * *",
			disruptionWindowDuration: "3h",
			want:                     true,
		},
		{
			name:                     "Active schedule, invalid duration",
			disruptionWindowSched:    "* * * * *",
			disruptionWindowDuration: "1h",
			want:                     true,
		},
		{
			name:                     "Inactive schedule",
			disruptionWindowSched:    fmt.Sprintf("%d %d * * *", time.Now().UTC().Minute(), time.Now().Add(-4*time.Hour).UTC().Hour()),
			disruptionWindowDuration: "3h",
			want:                     false,
		},
		{
			name:                  "Invalid schedule",
			disruptionWindowSched: "hello",
			want:                  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, controller.IsDisruptionWindowActive(context.Background(), podNamespace, podName, tt.disruptionWindowSched, tt.disruptionWindowDuration), "isDisruptionWindowActive(%v, %v, %v, %v)", podNamespace, podName, tt.disruptionWindowSched, tt.disruptionWindowDuration)
		})
	}
}
