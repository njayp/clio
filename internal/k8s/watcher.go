package k8s

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/njayp/clio"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Watcher watches pods in a Helm release and streams log lines as ErrorEvents.
type Watcher struct {
	client    kubernetes.Interface
	namespace string
	repo      string
	release   string
	target    string // optional: narrow to app.kubernetes.io/name
	tailLines int64
	out       chan clio.ErrorEvent
}

// NewWatcher creates a watcher for pods matching the given Helm release.
func NewWatcher(client kubernetes.Interface, cfg clio.Config) *Watcher {
	return &Watcher{
		client:    client,
		namespace: cfg.Namespace,
		repo:      cfg.Repo,
		release:   cfg.Release,
		target:    cfg.Target,
		tailLines: cfg.TailLines,
		out:       make(chan clio.ErrorEvent, 100),
	}
}

// Watch starts watching pods and streaming their logs. Returns a channel of single-line ErrorEvents.
func (w *Watcher) Watch(ctx context.Context) (<-chan clio.ErrorEvent, error) {
	labelSelector := labelSelectorForRelease(w.release, w.target).String()

	factory := informers.NewSharedInformerFactoryWithOptions(
		w.client, 30*time.Second,
		informers.WithNamespace(w.namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = labelSelector
		}),
	)

	podInformer := factory.Core().V1().Pods().Informer()
	tracked := make(map[string]context.CancelFunc)

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			if pod.Status.Phase != corev1.PodRunning {
				return
			}
			w.startTailing(ctx, pod, tracked)
		},
		UpdateFunc: func(_, newObj interface{}) {
			pod := newObj.(*corev1.Pod)
			key := pod.Namespace + "/" + pod.Name
			if pod.Status.Phase == corev1.PodRunning {
				if _, ok := tracked[key]; !ok {
					w.startTailing(ctx, pod, tracked)
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			key := pod.Namespace + "/" + pod.Name
			if cancel, ok := tracked[key]; ok {
				cancel()
				delete(tracked, key)
			}
		},
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	slog.Info("watcher started", "namespace", w.namespace, "selector", labelSelector)
	return w.out, nil
}

func (w *Watcher) startTailing(ctx context.Context, pod *corev1.Pod, tracked map[string]context.CancelFunc) {
	key := pod.Namespace + "/" + pod.Name
	if _, ok := tracked[key]; ok {
		return
	}

	podCtx, cancel := context.WithCancel(ctx)
	tracked[key] = cancel

	for _, container := range pod.Spec.Containers {
		go w.tailLogs(podCtx, pod.Name, pod.Namespace, container.Name)
	}
}

func (w *Watcher) tailLogs(ctx context.Context, podName, namespace, container string) {
	sinceTime := metav1.NewTime(time.Now())
	tailLines := w.tailLines

	for {
		opts := &corev1.PodLogOptions{
			Container: container,
			Follow:    true,
		}
		if tailLines > 0 {
			opts.TailLines = &tailLines
			tailLines = 0 // only use tail on first connect
		} else {
			opts.SinceTime = &sinceTime
		}

		stream, err := w.client.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("failed to stream logs, retrying",
				"pod", podName, "container", container, "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := scanner.Text()
			sinceTime = metav1.NewTime(time.Now())
			if !IsErrorLine(line) {
				continue
			}
			w.out <- clio.ErrorEvent{
				PodName:   podName,
				Namespace: namespace,
				Container: container,
				Repo:      w.repo,
				LogLines:  []string{line},
				Timestamp: time.Now(),
			}
		}
		stream.Close()

		if ctx.Err() != nil {
			return
		}
		slog.Info("log stream ended, reconnecting", "pod", podName, "container", container)
		time.Sleep(2 * time.Second)
	}
}

// GatherContext populates the K8sContext for an error event by reading
// pod events, owning deployment status, configmaps, and deployment history.
func (w *Watcher) GatherContext(ctx context.Context, event *clio.ErrorEvent) error {
	kctx := &clio.K8sContext{}

	// Get pod events
	events, err := w.client.CoreV1().Events(event.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", event.PodName),
	})
	if err == nil {
		for _, e := range events.Items {
			kctx.Events = append(kctx.Events, fmt.Sprintf("%s: %s", e.Reason, e.Message))
		}
	}

	// Get pod to find owning deployment
	pod, err := w.client.CoreV1().Pods(event.Namespace).Get(ctx, event.PodName, metav1.GetOptions{})
	if err != nil {
		event.K8sContext = kctx
		return nil // best effort
	}

	// Find the owning deployment via owner references (Pod → ReplicaSet → Deployment)
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			rs, err := w.client.AppsV1().ReplicaSets(event.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			for _, rsOwner := range rs.OwnerReferences {
				if rsOwner.Kind == "Deployment" {
					kctx.DeployName = rsOwner.Name
					deploy, err := w.client.AppsV1().Deployments(event.Namespace).Get(ctx, rsOwner.Name, metav1.GetOptions{})
					if err == nil {
						kctx.Replicas = int(*deploy.Spec.Replicas)

						// Get current image tag
						for _, c := range deploy.Spec.Template.Spec.Containers {
							if c.Name == event.Container {
								kctx.ImageTag = c.Image
								break
							}
						}

						// Gather deployment history from ReplicaSets
						w.gatherDeployHistory(ctx, event.Namespace, rsOwner.Name, rs.Name, kctx)

						// Collect mounted configmaps
						for _, vol := range deploy.Spec.Template.Spec.Volumes {
							if vol.ConfigMap != nil {
								kctx.ConfigMaps = append(kctx.ConfigMaps, vol.ConfigMap.Name)
							}
						}
					}
				}
			}
		}
	}

	event.K8sContext = kctx
	return nil
}

// gatherDeployHistory reads ReplicaSets owned by the deployment to find the
// previous revision's image tag and detect rollbacks.
func (w *Watcher) gatherDeployHistory(ctx context.Context, namespace, deployName, currentRSName string, kctx *clio.K8sContext) {
	// List ReplicaSets in namespace (filtered client-side by owner reference below)
	rsList, err := w.client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}

	type rsInfo struct {
		name     string
		revision int
		image    string
	}

	var owned []rsInfo
	for _, rs := range rsList.Items {
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" && ref.Name == deployName {
				rev, _ := strconv.Atoi(rs.Annotations["deployment.kubernetes.io/revision"])
				img := ""
				for _, c := range rs.Spec.Template.Spec.Containers {
					img = c.Image
					break
				}
				owned = append(owned, rsInfo{name: rs.Name, revision: rev, image: img})
			}
		}
	}

	if len(owned) < 2 {
		return
	}

	// Sort by revision descending
	sort.Slice(owned, func(i, j int) bool { return owned[i].revision > owned[j].revision })

	// Find current RS revision
	var currentRevision int
	for _, rs := range owned {
		if rs.name == currentRSName {
			currentRevision = rs.revision
			break
		}
	}

	// Previous is the second-highest revision
	maxRevision := owned[0].revision
	kctx.RolledBack = currentRevision < maxRevision

	// Find previous image (first entry that isn't the current RS)
	for _, rs := range owned {
		if rs.name != currentRSName {
			kctx.PrevImageTag = rs.image
			break
		}
	}
}

// labelSelectorForRelease builds a label selector string matching the release.
func labelSelectorForRelease(release, target string) labels.Selector {
	set := labels.Set{"app.kubernetes.io/instance": release}
	if target != "" {
		set["app.kubernetes.io/name"] = target
	}
	return set.AsSelector()
}
