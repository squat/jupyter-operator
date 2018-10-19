package k8sutil

import (
	"fmt"
	"reflect"
	"time"

	"github.com/Sirupsen/logrus"
	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"
	"github.com/squat/retry"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	v1beta1client "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	v1listers "k8s.io/client-go/listers/core/v1"
	v1beta1listers "k8s.io/client-go/listers/extensions/v1beta1"
)

// CalculateIngress creates a new k8s ingress struct configured for the given notebook.
func CalculateIngress(n *jupyterv1.Notebook) *extensionsv1beta1.Ingress {
	backend := extensionsv1beta1.IngressBackend{
		ServiceName: resourceName(n.Name),
		ServicePort: intstr.FromInt(notebookPort),
	}
	if n.Spec.Ingress != nil {
		backend = *n.Spec.Ingress
	}
	labels := sapyensLabels(n.Name, n.Spec.Owner)
	ing := extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName(n.Name),
			Namespace: n.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "nginx",
			},
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{
				{
					Host: n.Name + "." + *n.Spec.Host,
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Path:    "/",
									Backend: backend,
								},
							},
						},
					},
				},
			},
		},
	}
	if ShouldHaveCerts(n) {
		tls := extensionsv1beta1.IngressTLS{
			Hosts: []string{
				n.Name + "." + *n.Spec.Host,
			},
		}
		if n.Spec.TLS != nil && *n.Spec.TLS == jupyterv1.NotebookTLSAcme {
			tls.SecretName = fmt.Sprintf(notebookIngressTLSSecretNameTemplate, resourceName(n.Name))
			ing.Annotations["kubernetes.io/tls-acme"] = "true"
		} else {
			ing.Annotations["nginx.ingress.kubernetes.io/ssl-passthrough"] = "true"
		}
		ing.Spec.TLS = []extensionsv1beta1.IngressTLS{tls}
		ing.Annotations["nginx.ingress.kubernetes.io/backend-protocol"] = "HTTPS"
		ing.Annotations["nginx.ingress.kubernetes.io/ssl-redirect"] = "true"
	}
	addOwnerRefToObject(ing.GetObjectMeta(), n.AsOwner())
	return &ing
}

// CreateOrUpdateIngress will update the given ingress, if it already exists, or create it if it doesn't.
// This function will adopt matching resources that are managed by the operator.
func CreateOrUpdateIngress(c v1beta1client.IngressInterface, l v1beta1listers.IngressLister, ing *extensionsv1beta1.Ingress) error {
	i, err := l.Ingresses(ing.Namespace).Get(ing.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err := c.Create(ing)
		return err
	}
	if !isManagedByOperator(i.Labels) {
		return fmt.Errorf("refusing to adopt existing %s: not managed by this operator", reflect.TypeOf(i))
	}
	ing.ResourceVersion = i.ResourceVersion
	_, err = c.Update(ing)
	return err
}

// WaitForIngressTLSSecret will wait for the given notebook's TLS secret to be ready
// and return an error if it is not ready before the timeout.
func WaitForIngressTLSSecret(l v1listers.SecretLister, logger logrus.StdLogger, n *jupyterv1.Notebook) error {
	name := fmt.Sprintf(notebookIngressTLSSecretNameTemplate, fmt.Sprintf(notebookNameTemplate, n.Name))

	messages := retry.Retry(retry.ConstantBackOff{5 * time.Second}, retry.Timeout(1*time.Minute), func() error {
		_, err := l.Secrets(n.Namespace).Get(name)
		if err != nil {
			return err
		}
		return nil
	})

	for message := range messages {
		if message.Done {
			if message.Error != nil {
				logger.Printf("failed to create notebook ingress TLS secret %s: %v", name, message.Error)
				return message.Error
			}
			break
		}
		logger.Printf("ingress TLS secret %s is not yet ready: %v", name, message.Error)
	}
	logger.Printf("successfully created notebook ingress TLS secret %s", name)
	return nil
}

// DeleteIngress will delete the ingress that corresponds to the given notebook.
func DeleteIngress(c v1beta1client.IngressInterface, n *jupyterv1.Notebook) error {
	return c.Delete(resourceName(n.Name), &metav1.DeleteOptions{})
}
