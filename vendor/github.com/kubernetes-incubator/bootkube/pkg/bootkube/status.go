package bootkube

import (
	"fmt"
	"path"
	"reflect"
	"strings"
	"time"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	doesNotExist = "DoesNotExist"
)

func WaitUntilPodsRunning(pods []string, timeout time.Duration) error {
	sc, err := NewStatusController(pods)
	if err != nil {
		return err
	}
	sc.Run()

	if err := wait.Poll(5*time.Second, timeout, sc.AllRunning); err != nil {
		return fmt.Errorf("error while checking pod status: %v", err)
	}

	UserOutput("All self-hosted control plane components successfully started\n")
	return nil
}

type statusController struct {
	client        kubernetes.Interface
	podStore      cache.Store
	watchPods     []string
	lastPodPhases map[string]v1.PodPhase
}

func NewStatusController(pods []string) (*statusController, error) {
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &statusController{client: client, watchPods: pods}, nil
}

func (s *statusController) Run() {
	// TODO(yifan): Be more explicit about the labels so that we don't just
	// reply on the prefix of the pod name when looking for the pods we are interested.
	// E.g. For a scheduler pod, we will look for pods that has label `tier=control-plane`
	// and `component=kube-scheduler`.
	options := metav1.ListOptions{}
	podStore, podController := cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(lo metav1.ListOptions) (runtime.Object, error) {
				return s.client.Core().Pods(api.NamespaceSystem).List(options)
			},
			WatchFunc: func(lo metav1.ListOptions) (watch.Interface, error) {
				return s.client.Core().Pods(api.NamespaceSystem).Watch(options)
			},
		},
		&v1.Pod{},
		30*time.Minute,
		cache.ResourceEventHandlerFuncs{},
	)
	s.podStore = podStore
	go podController.Run(wait.NeverStop)
}

func (s *statusController) AllRunning() (bool, error) {
	ps, err := s.PodStatus()
	if err != nil {
		glog.Infof("Error retriving pod statuses: %v", err)
		return false, nil
	}

	if s.lastPodPhases == nil {
		s.lastPodPhases = ps
	}

	// use lastPodPhases to print only pods whose phase has changed
	changed := !reflect.DeepEqual(ps, s.lastPodPhases)
	s.lastPodPhases = ps

	running := true
	for p, s := range ps {
		if changed {
			UserOutput("\tPod Status:%24s\t%s\n", p, s)
		}
		if s != v1.PodRunning {
			running = false
		}
	}
	return running, nil
}

func (s *statusController) PodStatus() (map[string]v1.PodPhase, error) {
	status := make(map[string]v1.PodPhase)

	podNames := s.podStore.ListKeys()
	for _, pod := range s.watchPods {
		// Pod names are suffixed with random data. Match on prefix
		podName := pod
		for _, pn := range podNames {
			if strings.HasPrefix(pn, path.Join(api.NamespaceSystem, pod)) {
				podName = pn
			}
		}
		p, exists, err := s.podStore.GetByKey(podName)
		if err != nil {
			return nil, err
		}
		if !exists {
			status[pod] = doesNotExist
			continue
		}
		if p, ok := p.(*v1.Pod); ok {
			status[pod] = p.Status.Phase
		}
	}
	return status, nil
}
