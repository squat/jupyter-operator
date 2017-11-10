package controller

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"reflect"
	"time"

	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"
	"github.com/squat/jupyter-operator/pkg/client"
	jupyterv1informers "github.com/squat/jupyter-operator/pkg/informers/externalversions/jupyter/v1"
	jupyterv1listers "github.com/squat/jupyter-operator/pkg/listers/jupyter/v1"

	"github.com/Sirupsen/logrus"
	"github.com/hashicorp/terraform/helper/mutexkv"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	v1informers "k8s.io/client-go/informers/core/v1"
	v1beta1informers "k8s.io/client-go/informers/extensions/v1beta1"
	v1listers "k8s.io/client-go/listers/core/v1"
	v1beta1listers "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const initRetryInterval = 10 * time.Second

// Config is the configuration for a notebook controller.
type Config struct {
	CACert     *x509.Certificate
	Key        *rsa.PrivateKey
	Kubeconfig string
	Namespace  string
}

// Controller encapsulates the logic for reconciling the desired state of notebooks
// as defined by notebook CRDs and the state of notebook resources in the cluster.
type Controller struct {
	Config
	client client.Interface
	logger *logrus.Entry
	mutex  *mutexkv.MutexKV
	queue  workqueue.RateLimitingInterface

	informers map[reflect.Type]cache.SharedIndexInformer
	// notebookLister can list/get notebooks
	notebookLister jupyterv1listers.NotebookLister
	// ingressLister can list/get ingresses from the shared informer's store
	ingressLister v1beta1listers.IngressLister
	// podLister can list/get pods from the shared informer's store
	podLister v1listers.PodLister
	// secretLister can list/get secrets from the shared informer's store
	secretLister v1listers.SecretLister
	// serviceLister can list/get services from the shared informer's store
	serviceLister v1listers.ServiceLister
}

// New creates a new instance of a notebook controller and returns a pointer to the struct.
func New(cfg Config) *Controller {
	controller := &Controller{
		Config:    cfg,
		client:    client.New(cfg.Kubeconfig),
		informers: map[reflect.Type]cache.SharedIndexInformer{},
		logger:    logrus.WithField("pkg", "controller"),
		mutex:     mutexkv.NewMutexKV(),
		queue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "notebook"),
	}

	indexer := cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}

	informer := jupyterv1informers.NewNotebookInformer(controller.client.VersionedInterface(), cfg.Namespace, 0, indexer)
	controller.notebookLister = jupyterv1listers.NewNotebookLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&jupyterv1.Notebook{})] = informer

	informer = v1informers.NewPodInformer(controller.client, cfg.Namespace, 0, indexer)
	controller.podLister = v1listers.NewPodLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&v1.Pod{})] = informer

	informer = v1informers.NewSecretInformer(controller.client, cfg.Namespace, 0, indexer)
	controller.secretLister = v1listers.NewSecretLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&v1.Secret{})] = informer

	informer = v1informers.NewServiceInformer(controller.client, cfg.Namespace, 0, indexer)
	controller.serviceLister = v1listers.NewServiceLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&v1.Service{})] = informer

	informer = v1beta1informers.NewIngressInformer(controller.client, cfg.Namespace, 0, indexer)
	controller.ingressLister = v1beta1listers.NewIngressLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&v1beta1.Ingress{})] = informer

	return controller
}

// Run starts the reconciliation loop of the notebook controller.
func (c *Controller) Run(stop <-chan struct{}, workers int) error {
	defer c.queue.ShutDown()

	for {
		c.logger.Info("initializing CRD")
		err := c.initCRD()
		if err == nil {
			break
		}
		c.logger.Errorf("failed to initialize CRD: %v", err)
		c.logger.Infof("retying CRD initialization in %v", initRetryInterval)
		<-time.After(initRetryInterval)
	}

	for _, i := range c.informers {
		go i.Run(stop)
	}

	if err := c.watch(stop); err != nil {
		return fmt.Errorf("failed to start controller: %v", err)
	}

	c.addHandlers()

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stop)
	}

	<-stop
	return nil
}

func (c *Controller) initCRD() error {
	err := c.createCRD()
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			c.logger.Info("CRD already exists")
			return nil
		}
		return fmt.Errorf("failed to create CRD: %v", err)
	}
	return c.waitForCRD()
}

func (c *Controller) createCRD() error {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: jupyterv1.NotebookName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   jupyterv1.GroupName,
			Version: jupyterv1.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     jupyterv1.NotebookPlural,
				Kind:       jupyterv1.NotebookKind,
				ShortNames: jupyterv1.NotebookShortNames,
			},
		},
	}
	_, err := c.client.APIExtensionsInterface().ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	return err
}

func (c *Controller) waitForCRD() error {
	// wait for CRD being established
	err := wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
		crd, err := c.client.APIExtensionsInterface().ApiextensionsV1beta1().CustomResourceDefinitions().Get(jupyterv1.NotebookName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					c.logger.Info("CRD is ready")
					return true, nil
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					c.logger.Warnf("CRD is not ready: %v", cond.Reason)
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for CRD to be created: %v", err)
	}
	return nil
}

func (c *Controller) addHandlers() {
	for it, i := range c.informers {
		if it == reflect.TypeOf(&jupyterv1.Notebook{}) {
			i.AddEventHandler(
				cache.ResourceEventHandlerFuncs{
					AddFunc:    c.onAddNotebook,
					UpdateFunc: c.onUpdateNotebook,
					DeleteFunc: c.onDeleteNotebook,
				},
			)
			continue
		}
		i.AddEventHandler(
			cache.ResourceEventHandlerFuncs{
				AddFunc:    c.onAddObject,
				UpdateFunc: c.onUpdateObject,
				DeleteFunc: c.onDeleteObject,
			},
		)
	}
}

func (c *Controller) watch(stop <-chan struct{}) error {
	ok := true
	informers := map[reflect.Type]bool{}
	for it := range c.informers {
		informers[it] = cache.WaitForCacheSync(stop, c.informers[it].HasSynced)
	}
	for i := range informers {
		if !informers[i] {
			c.logger.Errorf("failed to sync %q cache", i)
			ok = false
		} else {
			c.logger.Debugf("successfully synced %q cache", i)
		}
	}
	if !ok {
		return errors.New("failed to sync caches")
	}
	c.logger.Info("successfully synced all caches")
	return nil
}

func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)
	err := c.sync(key.(string))
	if err != nil {
		c.logger.Errorf("failed processing %q: %v", key, err)
	}
	return true
}

func (c *Controller) enqueue(notebook *jupyterv1.Notebook) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(notebook)
	if err != nil {
		c.logger.Errorf("could not get key for object %#v: %v", notebook, err)
		return
	}
	c.logger.Debugf("queueing notebook %q", key)
	c.queue.Add(key)
}

func (c *Controller) sync(key string) error {
	c.mutex.Lock(key)
	defer c.mutex.Unlock(key)
	c.logger.Debugf("syncing notebook %q", key)
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Debugf("failed to split key %q", key)
		return err
	}
	notebook, err := c.notebookLister.Notebooks(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		c.logger.Infof("notebook %q has been deleted", key)
		// Until k8s 1.8, which brings garbage collection for CRDs, implement manual deletion.
		notebook = new(jupyterv1.Notebook)
		notebook.Name = name
		notebook.Namespace = namespace
		now := metav1.Now()
		notebook.DeletionTimestamp = &now
		if err := c.reconcileNotebookResources(notebook); err != nil {
			c.logger.Errorf("failed to reconcile deleted notebook: %v", err)
		}
		return nil
	}
	if err != nil {
		c.logger.Debugf("failed to list notebooks: %v", err)
		return err
	}

	n := notebook.DeepCopy()
	if n.Kind == "" {
		n.Kind = jupyterv1.NotebookKind
		n.APIVersion = jupyterv1.SchemeGroupVersion.String()
	}
	if n.Status.Phase != jupyterv1.NotebookPhaseRunning && n.Status.Phase != jupyterv1.NotebookPhaseFailed {
		if err = c.setNotebookPhase(n, jupyterv1.NotebookPhasePending); err != nil {
			c.logger.Warnf("failed to set notebook phase for %s: %v", key, err)
		}
	}
	if err = c.reconcileNotebookResources(n); err != nil {
		c.logger.Debugf("failed to reconcile resources for %s: %v", key, err)
		c.setNotebookPhase(n, jupyterv1.NotebookPhaseFailed)
		return err
	}
	if err = c.setNotebookPhase(n, jupyterv1.NotebookPhaseRunning); err != nil {
		c.logger.Warnf("failed to set notebook phase for %s: %v", key, err)
	}

	return nil
}

func (c *Controller) resolveOwnerRef(namespace string, ref *metav1.OwnerReference) *jupyterv1.Notebook {
	if ref == nil {
		return nil
	}
	// If the owner reference points at the wrong kind of object, bail.
	if ref.Kind != jupyterv1.NotebookKind {
		return nil
	}
	n, err := c.notebookLister.Notebooks(namespace).Get(ref.Name)
	if err != nil {
		c.logger.Debugf("could not list notebook in namespace %q with name %q", namespace, ref.Name)
		return nil
	}
	if n.UID != ref.UID {
		c.logger.Debugf("owner reference UID %q does not match notebook UID %q for notebook in namespace %q with name %q", ref.UID, n.UID, namespace, ref.Name)
		return nil
	}
	return n
}

func (c *Controller) getPodsForNotebook(notebook *jupyterv1.Notebook) ([]*v1.Pod, error) {
	return nil, nil
}
