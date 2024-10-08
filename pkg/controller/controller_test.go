package controller

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"

	"github.com/jukie/karpenter-deprovision-controller/pkg/clienthelpers"

	"github.com/awslabs/operatorpkg/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func setupTestNode(name string, creationTime time.Time, labels map[string]string, unschedulable bool) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.Time{Time: creationTime},
			Labels:            labels,
		},
		Spec: corev1.NodeSpec{
			ProviderID:    "testid",
			Unschedulable: unschedulable,
		},
	}
}

func setupTestNodeClaim(name, nodeName string, isExpired metav1.ConditionStatus) *karpv1.NodeClaim {
	return &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				karpv1.NodePoolLabelKey: "test-pool",
			},
		},
		Status: karpv1.NodeClaimStatus{
			NodeName: nodeName,
			Conditions: []status.Condition{{
				Type:   "Expired",
				Status: isExpired,
			}},
			ProviderID: "testid",
		},
	}
}

func TestReconcileNodeClaim(t *testing.T) {
	expiredNode := setupTestNode("expired-node", time.Now().AddDate(0, 0, -8).UTC(), nil, false)
	expireNodeClaim := setupTestNodeClaim("test-claim", expiredNode.Name, metav1.ConditionTrue)

	noBlockingAnnotationPod := setupTestPod("no-blocking-annotation", "testing", expiredNode.Name, nil)
	blockingNoSchedulePod := setupTestPod("blocking-no-sched", "testing", expiredNode.Name, map[string]string{
		karpv1.DoNotDisruptAnnotationKey: "true",
	})
	blockingInactiveSchedulePod := setupTestPod("blocking-inactive-sched", "testing", expiredNode.Name, map[string]string{
		karpv1.DoNotDisruptAnnotationKey: "true",
		disruptionWindowSchedKey:         fmt.Sprintf("0 %d * * *", time.Now().Add(-4*time.Hour).UTC().Hour()),
		disruptionWindowDurationKey:      "4h",
	})
	blockingActiveSchedulePod := setupTestPod("blocking-active-sched", "testing", expiredNode.Name, map[string]string{
		karpv1.DoNotDisruptAnnotationKey: "true",
		disruptionWindowSchedKey:         "* * * * *",
	})
	blockingInvalidSchedulePod := setupTestPod("blocking-invalid-sched", "testing", expiredNode.Name, map[string]string{
		karpv1.DoNotDisruptAnnotationKey: "true",
		disruptionWindowSchedKey:         "hello",
		disruptionWindowDurationKey:      "4h",
	})
	blockingInvalidDurationPod := setupTestPod("blocking-invalid-duration", "testing", expiredNode.Name, map[string]string{
		karpv1.DoNotDisruptAnnotationKey: "true",
		disruptionWindowSchedKey:         "* * * * *",
		disruptionWindowDurationKey:      "1h",
	})

	controller := &NodeClaimController{}
	scheme := clienthelpers.GetScheme()

	tests := []struct {
		name           string
		controller     *NodeClaimController
		pods           []*corev1.Pod
		nodeclaim      *karpv1.NodeClaim
		node           *corev1.Node
		objList        []runtime.Object
		expectPodPatch bool
	}{
		{
			name:           "No blocking annotation",
			controller:     controller,
			pods:           []*corev1.Pod{noBlockingAnnotationPod},
			nodeclaim:      expireNodeClaim,
			node:           expiredNode,
			objList:        []runtime.Object{noBlockingAnnotationPod, expiredNode, expireNodeClaim},
			expectPodPatch: false,
		},
		{
			name:           "Pod with blocking annotation and no schedule",
			controller:     controller,
			pods:           []*corev1.Pod{blockingNoSchedulePod},
			nodeclaim:      expireNodeClaim,
			node:           expiredNode,
			objList:        []runtime.Object{blockingNoSchedulePod, expiredNode, expireNodeClaim},
			expectPodPatch: true,
		},
		{
			name:           "Pod with blocking annotation and inactive schedule",
			controller:     controller,
			pods:           []*corev1.Pod{blockingInactiveSchedulePod},
			nodeclaim:      expireNodeClaim,
			node:           expiredNode,
			objList:        []runtime.Object{blockingInactiveSchedulePod, expiredNode, expireNodeClaim},
			expectPodPatch: false,
		},
		{
			name:           "Pod with blocking annotation and invalid schedule",
			controller:     controller,
			pods:           []*corev1.Pod{blockingInvalidSchedulePod},
			nodeclaim:      expireNodeClaim,
			node:           expiredNode,
			objList:        []runtime.Object{blockingInvalidSchedulePod, expiredNode, expireNodeClaim},
			expectPodPatch: true,
		},
		{
			name:           "Pod with blocking annotation and active schedule but invalid duration",
			controller:     controller,
			pods:           []*corev1.Pod{blockingInvalidDurationPod},
			nodeclaim:      expireNodeClaim,
			node:           expiredNode,
			objList:        []runtime.Object{blockingInvalidDurationPod, expiredNode, expireNodeClaim},
			expectPodPatch: true,
		},
		{
			name:           "Pod with blocking annotation and active schedule",
			controller:     controller,
			pods:           []*corev1.Pod{blockingActiveSchedulePod},
			nodeclaim:      expireNodeClaim,
			node:           expiredNode,
			objList:        []runtime.Object{blockingActiveSchedulePod, expiredNode, expireNodeClaim},
			expectPodPatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller.Client = fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.objList...).
				WithIndex(&corev1.Pod{}, "spec.nodeName", clienthelpers.PodIdxFunc).
				Build()
			_, err := controller.Reconcile(context.TODO(), tt.nodeclaim)
			assert.NoError(t, err)

			for _, pod := range tt.pods {
				updatedPod := &corev1.Pod{}
				err := controller.Client.Get(context.TODO(), client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, updatedPod)
				assert.NoError(t, err)
				if tt.expectPodPatch {
					assert.Equal(t, "", updatedPod.Annotations[karpv1.DoNotDisruptAnnotationKey], "Expected annotation to be removed")
				} else {
					assert.Equal(t, pod.Annotations[karpv1.DoNotDisruptAnnotationKey], updatedPod.Annotations[karpv1.DoNotDisruptAnnotationKey], "Expected annotation to be removed")
					assert.Equal(t, pod, updatedPod)
				}
			}
		})
	}
}

func TestHandleBlockingPods(t *testing.T) {
	expiredNodeName := "test-node"
	noBlockingAnnotationPod := setupTestPod("no-blocking-annotation", "testing", expiredNodeName, nil)
	blockingNoSchedulePod := setupTestPod("blocking-no-sched", "testing", expiredNodeName, map[string]string{karpv1.DoNotDisruptAnnotationKey: "true"})
	blockingInactiveSchedulePod := setupTestPod("blocking-inactive-sched", "testing", expiredNodeName, map[string]string{
		karpv1.DoNotDisruptAnnotationKey: "true",
		disruptionWindowSchedKey:         fmt.Sprintf("%d %d * * *", time.Now().UTC().Minute(), time.Now().Add(-4*time.Hour).UTC().Hour()),
		disruptionWindowDurationKey:      "4h",
	})
	blockingActiveSchedulePod := setupTestPod("blocking-active-sched", "testing", "test-node", map[string]string{
		karpv1.DoNotDisruptAnnotationKey: "true",
		disruptionWindowSchedKey:         fmt.Sprintf("%d %d * * *", time.Now().UTC().Minute(), time.Now().UTC().Hour()),
		disruptionWindowDurationKey:      "5h",
	})

	tests := []struct {
		name        string
		pods        []corev1.Pod
		objList     []runtime.Object
		expectPatch bool
	}{
		{
			name:        "No blocking annotation",
			pods:        []corev1.Pod{*noBlockingAnnotationPod},
			objList:     []runtime.Object{noBlockingAnnotationPod},
			expectPatch: false,
		},
		{
			name:        "Blocking annotation without schedule",
			pods:        []corev1.Pod{*blockingNoSchedulePod},
			objList:     []runtime.Object{blockingNoSchedulePod},
			expectPatch: true,
		},
		{
			name:        "Blocking annotation with inactive schedule",
			pods:        []corev1.Pod{*blockingInactiveSchedulePod},
			objList:     []runtime.Object{blockingInactiveSchedulePod},
			expectPatch: false,
		},
		{
			name:        "Blocking annotation with active schedule",
			pods:        []corev1.Pod{*blockingActiveSchedulePod},
			objList:     []runtime.Object{blockingActiveSchedulePod},
			expectPatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake client with the test pods

			fakeClient := fake.NewClientBuilder().WithRuntimeObjects(tt.objList...).Build()

			controller := &NodeClaimController{
				Client: fakeClient,
			}
			controller.handleBlockingPods(context.TODO(), tt.pods, "test-node")

			for _, pod := range tt.pods {
				updatedPod := &corev1.Pod{}
				err := fakeClient.Get(context.TODO(), client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, updatedPod)
				assert.NoError(t, err)
				if tt.expectPatch {
					assert.Equal(t, "", updatedPod.Annotations[karpv1.DoNotDisruptAnnotationKey], "Expected annotation to be removed")
				} else {
					assert.Equal(t, pod.Annotations[karpv1.DoNotDisruptAnnotationKey], updatedPod.Annotations[karpv1.DoNotDisruptAnnotationKey], "Expected do-not-disrupt annotation to be unchanged")
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
			assert.Equalf(t, tt.want, isDisruptionWindowActive(context.Background(), podNamespace, podName, tt.disruptionWindowSched, tt.disruptionWindowDuration), "isDisruptionWindowActive(%v, %v, %v, %v)", podNamespace, podName, tt.disruptionWindowSched, tt.disruptionWindowDuration)
		})
	}
}
