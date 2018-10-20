package k8sutil

import (
	"fmt"
	"reflect"
	"time"

	"github.com/Sirupsen/logrus"
	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"

	"github.com/go-test/deep"
	"github.com/squat/retry"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	v1listers "k8s.io/client-go/listers/apps/v1"
)

// CalculateStatefulSet creates a new k8s StatefulSet struct configured for the given notebook.
func CalculateStatefulSet(n *jupyterv1.Notebook) *appsv1.StatefulSet {
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
	var volumes []corev1.Volume
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
		volumes = append(volumes, corev1.Volume{
			Name: "tls",
			VolumeSource: corev1.VolumeSource{
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
			},
		})
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
	terminationGracePeriod := int64(120)
	var automountServiceAccountToken bool
	var serviceAccountName string
	if *n.Spec.ServiceAccountName != "" {
		automountServiceAccountToken = true
		serviceAccountName = *n.Spec.ServiceAccountName
	}
	podLabels := addMatchLabels(make(map[string]string), n.Name, n.Spec.Owner)
	pod := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: podLabels,
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken:  &automountServiceAccountToken,
			Containers:                    []corev1.Container{container},
			DNSPolicy:                     corev1.DNSClusterFirst,
			RestartPolicy:                 corev1.RestartPolicyAlways,
			SchedulerName:                 corev1.DefaultSchedulerName,
			SecurityContext:               &corev1.PodSecurityContext{},
			ServiceAccountName:            serviceAccountName,
			TerminationGracePeriodSeconds: &terminationGracePeriod,
			Tolerations:                   tolerations,
			Volumes:                       volumes,
		},
	}
	addOwnerRefToObject(pod.GetObjectMeta(), n.AsOwner())
	var replicas int32 = 1
	sts := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName(n.Name),
			Namespace: n.Namespace,
			Labels:    notebookLabels(n.Name, n.Spec.Owner),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: podLabels,
			},
			ServiceName: resourceName(n.Name),
			Template:    pod,
		},
	}
	return &sts
}

// CreateOrUpdateStatefulSet will update the given StatefulSet, if it already exists, or create it if it doesn't.
// This function will adopt matching resources that are managed by the operator.
func CreateOrUpdateStatefulSet(c v1client.StatefulSetInterface, l v1listers.StatefulSetLister, logger logrus.StdLogger, sts *appsv1.StatefulSet) error {
	s, err := l.StatefulSets(sts.Namespace).Get(sts.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err := c.Create(sts)
		return err
	}
	if !isManagedByOperator(s.Labels) {
		return fmt.Errorf("refusing to adopt existing %s: not managed by this operator", reflect.TypeOf(s))
	}
	templateDiff := deep.Equal(sts.Spec.Template, s.Spec.Template)
	// If the templates are the same, then adopt this StatefulSet.
	if templateDiff == nil {
		sts.ResourceVersion = s.ResourceVersion
		sts.Spec = s.Spec
		_, err = c.Update(sts)
		return err
	}
	// If there is a diff, delete the old pod and create a new one.
	dpp := metav1.DeletePropagationForeground
	if err = c.Delete(s.Name, &metav1.DeleteOptions{PropagationPolicy: &dpp}); err != nil {
		return fmt.Errorf("failed to delete old StatefulSet before updating: %v", err)
	}
	if err = WaitForStatefulSetDeleted(l, logger, s); err != nil {
		return fmt.Errorf("failed waiting for StatefulSet to be deleted: %v", err)
	}
	_, err = c.Create(sts)
	return err
}

// WaitForStatefulSet will wait for the given StatefulSet to be ready and return an error if it is not ready before the timeout.
func WaitForStatefulSet(l v1listers.StatefulSetLister, logger logrus.StdLogger, sts *appsv1.StatefulSet) error {
	messages := retry.Retry(retry.ConstantBackOff{5 * time.Second}, retry.Timeout(1*time.Minute), func() error {
		s, err := l.StatefulSets(sts.Namespace).Get(sts.Name)
		if err != nil {
			return err
		}

		if s.Status.ReadyReplicas != *sts.Spec.Replicas {
			return fmt.Errorf("ready replicas is %d, waiting for ready replicas to be %d", s.Status.ReadyReplicas, *sts.Spec.Replicas)
		}
		return nil
	})

	for message := range messages {
		if message.Done {
			if message.Error != nil {
				logger.Printf("failed to create notebook StatefulSet %s: %v", sts.Name, message.Error)
				return message.Error
			}
			break
		}
		logger.Printf("StatefulSet %s is not yet ready: %v", sts.Name, message.Error)
	}
	logger.Printf("successfully created notebook StatefulSet %s", sts.Name)
	return nil
}

// WaitForStatefulSetDeleted will wait for the given StatefulSet to be deleted return an error if it is not deleted before the timeout.
func WaitForStatefulSetDeleted(l v1listers.StatefulSetLister, logger logrus.StdLogger, sts *appsv1.StatefulSet) error {
	messages := retry.Retry(retry.ConstantBackOff{5 * time.Second}, retry.Timeout(1*time.Minute), func() error {
		_, err := l.StatefulSets(sts.Namespace).Get(sts.Name)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("waiting for StatefulSet to be deleted")
	})

	for message := range messages {
		if message.Done {
			if message.Error != nil {
				logger.Printf("failed to create notebook StatefulSet %s: %v", sts.Name, message.Error)
				return message.Error
			}
			break
		}
		logger.Printf("StatefulSet %s is not yet deleted: %v", sts.Name, message.Error)
	}
	logger.Printf("successfully deleted notebook StatefulSet %s", sts.Name)
	return nil
}

// DeleteStatefulSet will delete the StatefulSet that corresponds to the given notebook.
func DeleteStatefulSet(c v1client.StatefulSetInterface, n *jupyterv1.Notebook) error {
	return c.Delete(resourceName(n.Name), &metav1.DeleteOptions{})
}
