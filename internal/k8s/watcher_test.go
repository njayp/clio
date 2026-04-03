package k8s

import (
	"context"
	"testing"

	"github.com/njayp/clio"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"
)

func int32Ptr(i int32) *int32 { return &i }

func TestGatherContext_PopulatesFields(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: "staging"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "web", Image: "myapp:v2"}},
					Volumes: []corev1.Volume{
						{Name: "config", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "myapp-config"},
							},
						}},
					},
				},
			},
		},
	}

	currentRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "myapp-abc", Namespace: "staging",
			Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "myapp"}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "myapp:v2"}}},
			},
		},
	}

	prevRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "myapp-def", Namespace: "staging",
			Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "myapp"}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "myapp:v1"}}},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "myapp-abc-xyz", Namespace: "staging",
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "myapp-abc"}},
		},
	}

	event := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "evt1", Namespace: "staging"},
		InvolvedObject: corev1.ObjectReference{Name: "myapp-abc-xyz"},
		Reason:         "Started",
		Message:        "Started container web",
	}

	client := fake.NewSimpleClientset(deploy, currentRS, prevRS, pod, event)
	w := &Watcher{client: client, namespace: "staging"}

	ev := &clio.ErrorEvent{PodName: "myapp-abc-xyz", Namespace: "staging", Container: "web"}
	if err := w.GatherContext(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ev.K8sContext == nil {
		t.Fatal("K8sContext should not be nil")
	}
	if ev.K8sContext.DeployName != "myapp" {
		t.Errorf("DeployName = %q, want %q", ev.K8sContext.DeployName, "myapp")
	}
	if ev.K8sContext.Replicas != 3 {
		t.Errorf("Replicas = %d, want 3", ev.K8sContext.Replicas)
	}
	if ev.K8sContext.ImageTag != "myapp:v2" {
		t.Errorf("ImageTag = %q, want %q", ev.K8sContext.ImageTag, "myapp:v2")
	}
	if ev.K8sContext.PrevImageTag != "myapp:v1" {
		t.Errorf("PrevImageTag = %q, want %q", ev.K8sContext.PrevImageTag, "myapp:v1")
	}
	if ev.K8sContext.RolledBack {
		t.Error("RolledBack should be false (revision 2 is current and max)")
	}
	if len(ev.K8sContext.ConfigMaps) != 1 || ev.K8sContext.ConfigMaps[0] != "myapp-config" {
		t.Errorf("ConfigMaps = %v", ev.K8sContext.ConfigMaps)
	}
	if len(ev.K8sContext.Events) == 0 {
		t.Error("expected at least one event")
	}
}

func TestGatherContext_DetectsRollback(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: "staging"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "myapp:v1"}}},
			},
		},
	}

	// Current RS has revision 1 (rolled back from revision 2)
	currentRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "myapp-old", Namespace: "staging",
			Annotations:     map[string]string{"deployment.kubernetes.io/revision": "1"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "myapp"}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "myapp:v1"}}},
			},
		},
	}

	badRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "myapp-bad", Namespace: "staging",
			Annotations:     map[string]string{"deployment.kubernetes.io/revision": "2"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "myapp"}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "myapp:v2-bad"}}},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "myapp-old-xyz", Namespace: "staging",
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "myapp-old"}},
		},
	}

	client := fake.NewSimpleClientset(deploy, currentRS, badRS, pod)
	w := &Watcher{client: client, namespace: "staging"}

	ev := &clio.ErrorEvent{PodName: "myapp-old-xyz", Namespace: "staging", Container: "web"}
	if err := w.GatherContext(context.Background(), ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ev.K8sContext.RolledBack {
		t.Error("RolledBack should be true (current revision 1 < max revision 2)")
	}
	if ev.K8sContext.PrevImageTag != "myapp:v2-bad" {
		t.Errorf("PrevImageTag = %q, want %q", ev.K8sContext.PrevImageTag, "myapp:v2-bad")
	}
}

func TestGatherContext_NoPod(t *testing.T) {
	client := fake.NewSimpleClientset()
	w := &Watcher{client: client, namespace: "staging"}

	ev := &clio.ErrorEvent{PodName: "nonexistent", Namespace: "staging", Container: "web"}
	err := w.GatherContext(context.Background(), ev)
	if err != nil {
		t.Fatalf("expected nil error for best-effort, got: %v", err)
	}
	if ev.K8sContext == nil {
		t.Error("K8sContext should still be set (even if sparse)")
	}
}

func TestLabelSelectorForRelease(t *testing.T) {
	sel := labelSelectorForRelease("myapp", "")
	if !sel.Matches(labels.Set{"app.kubernetes.io/instance": "myapp"}) {
		t.Error("should match release label")
	}

	sel = labelSelectorForRelease("myapp", "web")
	if !sel.Matches(labels.Set{"app.kubernetes.io/instance": "myapp", "app.kubernetes.io/name": "web"}) {
		t.Error("should match release+target labels")
	}
	if sel.Matches(labels.Set{"app.kubernetes.io/instance": "myapp"}) {
		t.Error("should not match without target label")
	}
}
