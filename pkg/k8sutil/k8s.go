package k8sutil

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	notebookImageTemplate                = "jupyter/%s-notebook:latest"
	notebookContainerName                = "notebook"
	notebookPort                         = 8888
	notebookPortName                     = "notebook-port"
	notebookNameTemplate                 = "jupyter-notebook-%s"
	notebookIngressTLSSecretNameTemplate = "%s-tls"
	notebookPasswordKey                  = "password"
	notebookTLSMountPath                 = "/var/lib/tls"

	managedByOperatorLabel      = "managed-by"
	managedByOperatorLabelValue = "jupyter-operator"
	sapyensNotebookLabel        = "sapyens.org/notebook"
	sapyensOwnerLabel           = "sapyens.org/owner"
)

func resourceName(name string) string {
	return fmt.Sprintf(notebookNameTemplate, name)
}

func managedByOperatorLabels() map[string]string {
	return map[string]string{
		managedByOperatorLabel: managedByOperatorLabelValue,
	}
}

func isManagedByOperator(labels map[string]string) bool {
	value, ok := labels[managedByOperatorLabel]
	if ok && value == managedByOperatorLabelValue {
		return true
	}
	return false
}

func sapyensLabels(name, owner string) map[string]string {
	l := managedByOperatorLabels()
	l[sapyensNotebookLabel] = name
	l[sapyensOwnerLabel] = owner
	return l
}

func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}
