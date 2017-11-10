package controller

import (
	"reflect"

	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"
	"github.com/squat/jupyter-operator/pkg/k8sutil"

	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
)

type reconciler struct {
	reconcile func() error
	wait      func() error
}

func (c *Controller) reconcilers(n *jupyterv1.Notebook) map[reflect.Type]reconciler {
	return map[reflect.Type]reconciler{
		reflect.TypeOf(&extensionsv1beta1.Ingress{}): c.ingressReconciler(n),
		reflect.TypeOf(&corev1.Pod{}):                c.podReconciler(n),
		reflect.TypeOf(&corev1.Secret{}):             c.secretReconciler(n),
		reflect.TypeOf(&corev1.Service{}):            c.serviceReconciler(n),
	}

}

func (c *Controller) ingressReconciler(n *jupyterv1.Notebook) reconciler {
	r := reconciler{}
	if ingressShouldExist(n) {
		ing := k8sutil.CalculateIngress(n)
		r.reconcile = func() error {
			return k8sutil.CreateOrUpdateIngress(c.client.ExtensionsV1beta1().Ingresses(n.Namespace), c.ingressLister, ing)
		}
		if n.Spec.TLS == nil || *n.Spec.TLS == jupyterv1.NotebookTLSAcme {
			r.wait = func() error {
				return k8sutil.WaitForIngressTLSSecret(c.secretLister, c.logger, n)
			}
		}
	} else {
		r.reconcile = func() error { return k8sutil.DeleteIngress(c.client.ExtensionsV1beta1().Ingresses(n.Namespace), n) }
	}
	return r
}

func (c *Controller) podReconciler(n *jupyterv1.Notebook) reconciler {
	r := reconciler{}
	if podShouldExist(n) {
		pod := k8sutil.CalculatePod(n)
		r.reconcile = func() error {
			return k8sutil.CreateOrUpdatePod(c.client.CoreV1().Pods(n.Namespace), c.podLister, c.logger, pod)
		}
		r.wait = func() error { return k8sutil.WaitForPod(c.podLister, c.logger, pod) }
	} else {
		r.reconcile = func() error { return k8sutil.DeletePod(c.client.CoreV1().Pods(n.Namespace), n) }
	}
	return r
}

func (c *Controller) secretReconciler(n *jupyterv1.Notebook) reconciler {
	r := reconciler{}
	if secretShouldExist(n) {
		secret := k8sutil.CalculateSecret(n, c.Config.CACert, c.Config.Key)
		r.reconcile = func() error {
			return k8sutil.CreateOrUpdateSecret(c.client.CoreV1().Secrets(n.Namespace), c.secretLister, c.Config.CACert, secret)
		}
	} else {
		r.reconcile = func() error { return k8sutil.DeleteSecret(c.client.CoreV1().Secrets(n.Namespace), n) }
	}
	return r
}

func (c *Controller) serviceReconciler(n *jupyterv1.Notebook) reconciler {
	r := reconciler{}
	if serviceShouldExist(n) {
		svc := k8sutil.CalculateService(n)
		r.reconcile = func() error {
			return k8sutil.CreateOrUpdateService(c.client.CoreV1().Services(n.Namespace), c.serviceLister, svc)
		}
	} else {
		r.reconcile = func() error { return k8sutil.DeleteService(c.client.CoreV1().Services(n.Namespace), n) }
	}
	return r
}

func podShouldExist(n *jupyterv1.Notebook) bool {
	if n == nil || n.DeletionTimestamp != nil {
		return false
	}
	return true
}

func ingressShouldExist(n *jupyterv1.Notebook) bool {
	if n == nil || n.DeletionTimestamp != nil {
		return false
	}
	if n.Spec.Host == nil || *n.Spec.Host == "" {
		return false
	}
	return true
}

func secretShouldExist(n *jupyterv1.Notebook) bool {
	if n == nil || n.DeletionTimestamp != nil {
		return false
	}
	if n.Spec.Password != nil && *n.Spec.Password != "" || k8sutil.ShouldHaveCerts(n) {
		return true
	}
	return false
}

func serviceShouldExist(n *jupyterv1.Notebook) bool {
	if n == nil || n.DeletionTimestamp != nil {
		return false
	}
	return true
}
