package k8sutil

import (
	"fmt"
	"reflect"
	"time"

	"github.com/Sirupsen/logrus"
	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"

	"github.com/go-test/deep"
	"github.com/squat/retry"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1client "k8s.io/client-go/kubernetes/typed/core/v1"
	v1listers "k8s.io/client-go/listers/core/v1"
)

// CalculatePod creates a new k8s pod struct configured for the given notebook.
func CalculatePod(n *jupyterv1.Notebook) *corev1.Pod {
	container := corev1.Container{
		Args:            []string{"start-notebook.sh", "--NotebookApp.token="},
		Image:           fmt.Sprintf(notebookImageTemplate, *n.Spec.Flavor),
		ImagePullPolicy: corev1.PullAlways,
		Name:            notebookContainerName,
		Ports: []corev1.ContainerPort{
			{
				Name:          notebookPortName,
				ContainerPort: notebookPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
	if n.Spec.Password != nil && *n.Spec.Password != "" {
		envVar := corev1.EnvVar{
			Name: "PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: resourceName(n.Name),
					},
					Key: notebookPasswordKey,
				},
			},
		}
		container.Env = []corev1.EnvVar{envVar}
		container.Args = append(container.Args, "--NotebookApp.password=\"$(PASSWORD)\"")
	}
	volume := corev1.Volume{
		Name: "tls",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				SizeLimit: resource.NewQuantity(0, resource.DecimalSI),
			},
		},
	}
	if ShouldHaveCerts(n) {
		container.Args = append(container.Args, "--NotebookApp.certfile="+notebookTLSMountPath+"/cert")
		container.Args = append(container.Args, "--NotebookApp.keyfile="+notebookTLSMountPath+"/key")
		container.VolumeMounts = []corev1.VolumeMount{
			{
				Name:      "tls",
				ReadOnly:  true,
				MountPath: notebookTLSMountPath,
			},
		}
		mode := int32(420)
		volume.VolumeSource = corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				DefaultMode: &mode,
				Items: []corev1.KeyToPath{
					{
						Key:  corev1.TLSCertKey,
						Path: "cert",
					},
					{
						Key:  corev1.TLSPrivateKeyKey,
						Path: "key",
					},
				},
				SecretName: resourceName(n.Name),
			},
		}
	}
	var tolerations []corev1.Toleration
	if n.Spec.GPU {
		container.Resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"nvidia.com/gpu": *resource.NewQuantity(1, resource.DecimalExponent),
			},
		}
		tolerations = append(tolerations, corev1.Toleration{
			Key:      "nvidia.com/gpu",
			Effect:   corev1.TaintEffectNoSchedule,
			Operator: corev1.TolerationOpExists,
		})
	}
	var automountServiceAccountToken bool
	var serviceAccountName string
	if n.Spec.ServiceAccountName != nil {
		automountServiceAccountToken = true
		serviceAccountName = *n.Spec.ServiceAccountName
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        resourceName(n.Name),
			Namespace:   n.Namespace,
			Labels:      sapyensLabels(n.Name, n.Spec.Owner),
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: &automountServiceAccountToken,
			Containers:                   []corev1.Container{container},
			RestartPolicy:                corev1.RestartPolicyNever,
			ServiceAccountName:           serviceAccountName,
			Tolerations:                  tolerations,
			Volumes:                      []corev1.Volume{volume},
		},
	}
	addOwnerRefToObject(pod.GetObjectMeta(), n.AsOwner())
	return &pod
}

// CreateOrUpdatePod will update the given pod, if it already exists, or create it if it doesn't.
// This function will adopt matching resources that are managed by the operator.
func CreateOrUpdatePod(c v1client.PodInterface, l v1listers.PodLister, logger logrus.StdLogger, pod *corev1.Pod) error {
	p, err := l.Pods(pod.Namespace).Get(pod.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err := c.Create(pod)
		return err
	}
	if !isManagedByOperator(p.Labels) {
		return fmt.Errorf("refusing to adopt existing %s: not managed by this operator", reflect.TypeOf(p))
	}
	containerDiff := deep.Equal(pod.Spec.Containers, p.Spec.Containers)
	volumeDiff := deep.Equal(pod.Spec.Volumes, p.Spec.Volumes)
	// If the containers and volumes are the same, then adopt this pod.
	if containerDiff == nil && volumeDiff == nil {
		pod.ResourceVersion = p.ResourceVersion
		pod.Spec = p.Spec
		_, err = c.Update(pod)
		return err
	}
	// If there is a diff, delete the old pod and create a new one.
	dpp := metav1.DeletePropagationForeground
	if err = c.Delete(p.Name, &metav1.DeleteOptions{PropagationPolicy: &dpp}); err != nil {
		return fmt.Errorf("failed to delete old pod before updating: %v", err)
	}
	if err = WaitForPodDeleted(l, logger, p); err != nil {
		return fmt.Errorf("failed waiting for pod to be deleted: %v", err)
	}
	_, err = c.Create(pod)
	return err
}

// WaitForPod will wait for the given pod to be ready and return an error if it is not ready before the timeout.
func WaitForPod(l v1listers.PodLister, logger logrus.StdLogger, pod *corev1.Pod) error {
	messages := retry.Retry(retry.ConstantBackOff{5 * time.Second}, retry.Timeout(1*time.Minute), func() error {
		p, err := l.Pods(pod.Namespace).Get(pod.Name)
		if err != nil {
			return err
		}
		if p.Status.Phase != corev1.PodRunning {
			return fmt.Errorf("pod phase is %q, waiting for pod to be %q", p.Status.Phase, corev1.PodRunning)
		}
		return nil
	})

	for message := range messages {
		if message.Done {
			if message.Error != nil {
				logger.Printf("failed to create notebook pod %s: %v", pod.Name, message.Error)
				return message.Error
			}
			break
		}
		logger.Printf("pod %s is not yet ready: %v", pod.Name, message.Error)
	}
	logger.Printf("successfully created notebook pod %s", pod.Name)
	return nil
}

// WaitForPodDeleted will wait for the given pod to be deleted return an error if it is not deleted before the timeout.
func WaitForPodDeleted(l v1listers.PodLister, logger logrus.StdLogger, pod *corev1.Pod) error {
	messages := retry.Retry(retry.ConstantBackOff{5 * time.Second}, retry.Timeout(1*time.Minute), func() error {
		_, err := l.Pods(pod.Namespace).Get(pod.Name)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("waiting for pod to be deleted")
	})

	for message := range messages {
		if message.Done {
			if message.Error != nil {
				logger.Printf("failed to create notebook pod %s: %v", pod.Name, message.Error)
				return message.Error
			}
			break
		}
		logger.Printf("pod %s is not yet deleted: %v", pod.Name, message.Error)
	}
	logger.Printf("successfully deleted notebook pod %s", pod.Name)
	return nil
}

// DeletePod will delete the pod that corresponds to the given notebook.
func DeletePod(c v1client.PodInterface, n *jupyterv1.Notebook) error {
	return c.Delete(resourceName(n.Name), &metav1.DeleteOptions{})
}
