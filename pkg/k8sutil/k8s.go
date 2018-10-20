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

func addManagedByOperatorLabels(labels map[string]string) map[string]string {
	labels[managedByOperatorLabel] = managedByOperatorLabelValue
	return labels
}

func isManagedByOperator(labels map[string]string) bool {
	value, ok := labels[managedByOperatorLabel]
	if ok && value == managedByOperatorLabelValue {
		return true
	}
	return false
}

func addSapyensLabels(labels map[string]string, name, owner string) map[string]string {
	labels[sapyensNotebookLabel] = name
	labels[sapyensOwnerLabel] = owner
	return labels
}

func sapyensLabels(name, owner string) map[string]string {
	l := make(map[string]string)
	addManagedByOperatorLabels(l)
	addSapyensLabels(l, name, owner)
	return l
}

func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}
