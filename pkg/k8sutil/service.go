package k8sutil

import (
	"fmt"
	"reflect"

	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1client "k8s.io/client-go/kubernetes/typed/core/v1"
	v1listers "k8s.io/client-go/listers/core/v1"
)

const tolerateUnreadyEndpointsAnnotation = "service.alpha.kubernetes.io/tolerate-unready-endpoints"

// CalculateService creates a new k8s service struct configured for the given notebook.
func CalculateService(n *jupyterv1.Notebook) *corev1.Service {
	labels := sapyensLabels(n.Name, n.Spec.Owner)
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName(n.Name),
			Namespace: n.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				tolerateUnreadyEndpointsAnnotation: "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     notebookPortName,
					Port:     notebookPort,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: addSapyensLabels(make(map[string]string), n.Name, n.Spec.Owner),
			Type:     corev1.ServiceTypeClusterIP,
		},
	}
	addOwnerRefToObject(svc.GetObjectMeta(), n.AsOwner())
	return &svc
}

// CreateOrUpdateService will update the given service, if it already exists, or create it if it doesn't.
// This function will adopt matching resources that are managed by the operator.
func CreateOrUpdateService(c v1client.ServiceInterface, l v1listers.ServiceLister, svc *corev1.Service) error {
	s, err := l.Services(svc.Namespace).Get(svc.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err := c.Create(svc)
		return err
	}
	if !isManagedByOperator(s.Labels) {
		return fmt.Errorf("refusing to adopt existing %s: not managed by this operator", reflect.TypeOf(s))
	}
	svc.ResourceVersion = s.ResourceVersion
	svc.Spec.ClusterIP = s.Spec.ClusterIP
	_, err = c.Update(svc)
	return err
}

// DeleteService will delete the service that corresponds to the given notebook.
func DeleteService(c v1client.ServiceInterface, n *jupyterv1.Notebook) error {
	return c.Delete(resourceName(n.Name), &metav1.DeleteOptions{})
}
