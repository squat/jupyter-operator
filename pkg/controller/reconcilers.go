package controller

import (
	"reflect"

	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"
	"github.com/squat/jupyter-operator/pkg/k8sutil"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
)

type reconciler struct {
	reconcile    func() error
	resourceType reflect.Type
	wait         func() error
}

func (c *Controller) reconcilers(n *jupyterv1.Notebook) []reconciler {
	return []reconciler{
		c.serviceReconciler(n),
		c.secretReconciler(n),
		c.statefulSetReconciler(n),
		c.ingressReconciler(n),
	}
}

func (c *Controller) ingressReconciler(n *jupyterv1.Notebook) reconciler {
	r := reconciler{resourceType: reflect.TypeOf(&extensionsv1beta1.Ingress{})}
	if ingressShouldExist(n) {
		ing := k8sutil.CalculateIngress(n)
		r.reconcile = func() error {
			return k8sutil.CreateOrUpdateIngress(c.client.ExtensionsV1beta1().Ingresses(n.Namespace), c.ingressLister, ing)
		}
		if n.Spec.TLS != nil && *n.Spec.TLS == jupyterv1.NotebookTLSAcme {
			r.wait = func() error {
				return k8sutil.WaitForIngressTLSSecret(c.secretLister, c.logger, n)
			}
		}
	} else {
		r.reconcile = func() error { return k8sutil.DeleteIngress(c.client.ExtensionsV1beta1().Ingresses(n.Namespace), n) }
	}
	return r
}

func (c *Controller) statefulSetReconciler(n *jupyterv1.Notebook) reconciler {
	r := reconciler{resourceType: reflect.TypeOf(&appsv1.StatefulSet{})}
	if statefulSetShouldExist(n) {
		sts := k8sutil.CalculateStatefulSet(n)
		r.reconcile = func() error {
			return k8sutil.CreateOrUpdateStatefulSet(c.client.Apps().StatefulSets(n.Namespace), c.statefulSetLister, c.logger, sts)
		}
		r.wait = func() error { return k8sutil.WaitForStatefulSet(c.statefulSetLister, c.logger, sts) }
	} else {
		r.reconcile = func() error { return k8sutil.DeleteStatefulSet(c.client.Apps().StatefulSets(n.Namespace), n) }
	}
	return r
}

func (c *Controller) secretReconciler(n *jupyterv1.Notebook) reconciler {
	r := reconciler{resourceType: reflect.TypeOf(&corev1.Secret{})}
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
	r := reconciler{resourceType: reflect.TypeOf(&corev1.Service{})}
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

func statefulSetShouldExist(n *jupyterv1.Notebook) bool {
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
