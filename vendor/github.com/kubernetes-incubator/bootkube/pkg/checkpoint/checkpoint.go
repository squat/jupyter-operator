// Package checkpoint provides libraries that are used by the pod-checkpointer utility to checkpoint
// pods on a node. See cmd/checkpoint/README.md in this repository for more information.
package checkpoint

import (
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

const (
	activeCheckpointPath    = "/etc/kubernetes/manifests"
	inactiveCheckpointPath  = "/etc/kubernetes/inactive-manifests"
	checkpointSecretPath    = "/etc/kubernetes/checkpoint-secrets"
	checkpointConfigMapPath = "/etc/kubernetes/checkpoint-configmaps"

	shouldCheckpointAnnotation = "checkpointer.alpha.coreos.com/checkpoint"    // = "true"
	checkpointParentAnnotation = "checkpointer.alpha.coreos.com/checkpoint-of" // = "podName"
	podSourceAnnotation        = "kubernetes.io/config.source"

	shouldCheckpoint = "true"
	podSourceFile    = "file"

	defaultPollingFrequency  = 3 * time.Second
	defaultCheckpointTimeout = 1 * time.Minute
)

var (
	lastCheckpoint time.Time
)

// Options defines the parameters that are required to start the checkpointer.
type Options struct {
	// CheckpointerPod holds information about this checkpointer pod.
	CheckpointerPod CheckpointerPod
	// KubeConfig is a valid kubeconfig for communicating with the APIServer.
	KubeConfig *restclient.Config
	// RemoteRuntimeEndpoint is the location of the CRI GRPC endpoint.
	RemoteRuntimeEndpoint string
	// RuntimeRequestTimeout is the timeout that is used for requests to the RemoteRuntimeEndpoint.
	RuntimeRequestTimeout time.Duration
}

// CheckpointerPod holds information about this checkpointer pod.
type CheckpointerPod struct {
	// The name of the node this checkpointer is running on.
	NodeName string
	// The name of the pod that is running this checkpointer.
	PodName string
	// The namespace of the pod that is running this checkpointer.
	PodNamespace string
}

// checkpointer holds state used by the checkpointer to perform its duties.
type checkpointer struct {
	apiserver       kubernetes.Interface
	kubelet         *kubeletClient
	cri             *remoteRuntimeService
	checkpointerPod CheckpointerPod
}

// Run instantiates and starts a new checkpointer. Returns error if there was a problem creating
// the checkpointer, otherwise never returns.
func Run(opts Options) error {
	apiserver := kubernetes.NewForConfigOrDie(opts.KubeConfig)

	kubelet, err := newKubeletClient(opts.KubeConfig)
	if err != nil {
		return fmt.Errorf("failed to load kubelet client: %v", err)
	}

	// Open a GRPC connection to the CRI shim
	cri, err := newRemoteRuntimeService(opts.RemoteRuntimeEndpoint, opts.RuntimeRequestTimeout)
	if err != nil {
		return fmt.Errorf("failed to connect to CRI server: %v", err)
	}

	cp := &checkpointer{
		apiserver:       apiserver,
		kubelet:         kubelet,
		cri:             cri,
		checkpointerPod: opts.CheckpointerPod,
	}
	cp.run()

	return nil
}

// run is the main checkpointing loop.
func (c *checkpointer) run() {
	for {
		time.Sleep(defaultPollingFrequency)

		// We must use both the :10255/pods endpoint and CRI shim, because /pods
		// endpoint could have stale data. The /pods endpoint will only show the last cached
		// status which has successfully been written to an apiserver. However, if there is
		// no apiserver, we may get stale state (e.g. saying pod is running, when it really is
		// not).
		localParentPods := c.kubelet.localParentPods()
		localRunningPods := c.cri.localRunningPods()

		c.createCheckpointsForValidParents(localParentPods)

		// Try to get scheduled pods from the apiserver.
		// These will be used to GC checkpoints for parents no longer scheduled to this node.
		// A return value of nil is assumed to be "could not contact apiserver"
		// TODO(aaron): only check this every 30 seconds or so
		apiParentPods := c.getAPIParentPods(c.checkpointerPod.NodeName)

		// Get on disk copies of (in)active checkpoints
		//TODO(aaron): Could be racy to load from disk each time, but much easier than trying to keep in-memory state in sync.
		activeCheckpoints := getFileCheckpoints(activeCheckpointPath)
		inactiveCheckpoints := getFileCheckpoints(inactiveCheckpointPath)

		start, stop, remove := process(localRunningPods, localParentPods, apiParentPods, activeCheckpoints, inactiveCheckpoints, c.checkpointerPod)

		// Handle remove at last because we may still have some work to do
		// before removing the checkpointer itself.
		handleStop(stop)
		handleStart(start)
		handleRemove(remove)
	}
}
