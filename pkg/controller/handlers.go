package controller

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type notebookResource struct {
	reconcile func() error
	wait      func() error
	resource  string
}

func (c *Controller) reconcileNotebookResources(n *jupyterv1.Notebook) error {
	rs := c.reconcilers(n)

	var wg sync.WaitGroup
	wg.Add(len(rs))
	errs := make([]error, len(rs))

	var i int
	for resourceType := range rs {
		go func(resourceType reflect.Type, r reconciler, err *error) {
			defer wg.Done()
			// Try to reconcile the resource.
			if r.reconcile != nil {
				if *err = r.reconcile(); *err != nil {
					if *err = joinErrors([]error{*err}, apierrors.IsNotFound, apierrors.IsAlreadyExists); *err != nil {
						c.logger.Errorf("failed to reconcile %s for %s: %v", resourceType, n.Name, *err)
						return
					}
					c.logger.Debugf("%s for %s is already reconciled", resourceType, n.Name)
					*err = nil
				}
				c.logger.Infof("reconciled %s for %s", resourceType, n.Name)
			}

			// Try to wait for the resource.
			if r.wait != nil {
				if *err = r.wait(); *err != nil {
					c.logger.Errorf("failed waiting for %s for %s to be ready: %v", resourceType, n.Name, *err)
					return
				}
				c.logger.Infof("%s for %s is ready", resourceType, n.Name)
			}
		}(resourceType, rs[resourceType], &errs[i])
		i++
	}
	wg.Wait()
	if err := joinErrors(errs); err != nil {
		return fmt.Errorf("failed to reconcile notebook resources for %s: %v", n.Name, err)
	}
	return nil
}

func (c *Controller) setNotebookPhase(n *jupyterv1.Notebook, phase jupyterv1.NotebookPhase) error {
	if n.Status.Phase == phase {
		return nil
	}
	n.Status.Phase = phase
	c.logger.Infof("setting notebook phase to %q", phase)
	_, err := c.client.VersionedInterface().JupyterV1().Notebooks(n.Namespace).Update(n)
	return err
}

func joinErrors(errs []error, ignore ...func(error) bool) error {
	var filteredErrs []string
Error:
	for i := range errs {
		if errs[i] != nil {
			for j := range ignore {
				if ignore[j](errs[i]) {
					continue Error
				}
			}
			filteredErrs = append(filteredErrs, errs[i].Error())
		}
	}
	if len(filteredErrs) != 0 {
		return errors.New(strings.Join(filteredErrs, "; "))
	}
	return nil
}

func (c *Controller) onAddNotebook(obj interface{}) {
	n, ok := obj.(*jupyterv1.Notebook)
	if !ok {
		c.logger.Warn("got broken notebook, ignoring...")
		return
	}
	c.logger.Infof("adding notebook %s", n.SelfLink)
	c.enqueue(n)
}

func (c *Controller) onUpdateNotebook(old, cur interface{}) {
	oldN, ok := old.(*jupyterv1.Notebook)
	if !ok {
		c.logger.Warn("got broken notebook, ignoring...")
		return
	}
	curN, ok := cur.(*jupyterv1.Notebook)
	if !ok {
		c.logger.Warn("got broken notebook, ignoring...")
		return
	}
	c.logger.Infof("updating notebook %s", oldN.SelfLink)
	c.enqueue(curN)
}

func (c *Controller) onDeleteNotebook(obj interface{}) {
	n, ok := obj.(*jupyterv1.Notebook)
	if !ok {
		c.logger.Warn("got broken notebook, ignoring...")
		return
	}
	c.logger.Infof("deleting notebook %s", n.SelfLink)
	c.enqueue(n)
}

func (c *Controller) onAddObject(obj interface{}) {
	o, ok := obj.(metav1.Object)
	if !ok {
		c.logger.Warn("expected a metav1 Object")
		c.logger.Warn(1, obj)
		return
	}
	if o.GetDeletionTimestamp() != nil {
		// On a restart of the controller, it's possible for an object to
		// show up in a state that is already pending deletion.
		c.onDeleteObject(obj)
		return
	}
	ownerRef := metav1.GetControllerOf(o)
	if ownerRef == nil {
		return
	}
	n := c.resolveOwnerRef(o.GetNamespace(), ownerRef)
	if n == nil {
		return
	}
	c.logger.Debugf("%T %q in namespace %q was added", obj, o.GetName(), o.GetNamespace())
	c.enqueue(n)
}

func (c *Controller) onUpdateObject(old, cur interface{}) {
	oldO, ok := old.(metav1.Object)
	if !ok {
		c.logger.Warn("expected a metav1 Object")
		c.logger.Warn(3, old)
		return
	}
	curO, ok := cur.(metav1.Object)
	if !ok {
		c.logger.Warn("expected a metav1 Object")
		c.logger.Warn(4, cur)
		return
	}
	if oldO.GetResourceVersion() == curO.GetResourceVersion() {
		return
	}
	oldOwnerRef := metav1.GetControllerOf(oldO)
	curOwnerRef := metav1.GetControllerOf(curO)
	ownerRefChanged := !reflect.DeepEqual(oldOwnerRef, curOwnerRef)
	if ownerRefChanged && oldOwnerRef != nil {
		// If the owner ref changed, then sync the old notebook.
		if n := c.resolveOwnerRef(oldO.GetNamespace(), oldOwnerRef); n != nil {
			c.logger.Debugf("%T %q in namespace %q was updated", old, oldO.GetName(), oldO.GetNamespace())
			c.enqueue(n)
		}
	}
	n := c.resolveOwnerRef(curO.GetNamespace(), curOwnerRef)
	if n == nil {
		return
	}
	c.logger.Debugf("%T %q in namespace %q was updated", cur, curO.GetName(), curO.GetNamespace())
	c.enqueue(n)
}

func (c *Controller) onDeleteObject(obj interface{}) {
	o, ok := obj.(metav1.Object)
	if !ok {
		c.logger.Warn("expected a metav1 Object")
		c.logger.Warn(7, obj)
		return
	}
	ownerRef := metav1.GetControllerOf(o)
	if ownerRef == nil {
		return
	}
	n := c.resolveOwnerRef(o.GetNamespace(), ownerRef)
	if n == nil {
		return
	}
	c.logger.Debugf("%T %q in namespace %q was deleted", obj, o.GetName(), o.GetNamespace())
	c.enqueue(n)
}
