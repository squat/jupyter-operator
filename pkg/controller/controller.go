package controller

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/go-test/deep"
	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"
	"github.com/squat/jupyter-operator/pkg/client"
	jupyterv1informers "github.com/squat/jupyter-operator/pkg/informers/externalversions/jupyter/v1"
	jupyterv1listers "github.com/squat/jupyter-operator/pkg/listers/jupyter/v1"

	"github.com/Sirupsen/logrus"
	crdutils "github.com/ant31/crd-validation/pkg"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	v1informers "k8s.io/client-go/informers/core/v1"
	v1beta1informers "k8s.io/client-go/informers/extensions/v1beta1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	v1listers "k8s.io/client-go/listers/core/v1"
	v1beta1listers "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	resyncPeriod = 5 * time.Minute
)

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
	queue  workqueue.RateLimitingInterface

	informers map[reflect.Type]cache.SharedIndexInformer
	// notebookLister can list/get notebooks
	notebookLister jupyterv1listers.NotebookLister
	// ingressLister can list/get Ingresses from the shared informer's store
	ingressLister v1beta1listers.IngressLister
	// statefulSetLister can list/get StatefulSets from the shared informer's store
	statefulSetLister appsv1listers.StatefulSetLister
	// secretLister can list/get Secrets from the shared informer's store
	secretLister v1listers.SecretLister
	// serviceLister can list/get Services from the shared informer's store
	serviceLister v1listers.ServiceLister
}

// New creates a new instance of a notebook controller and returns a pointer to the struct.
func New(cfg Config) *Controller {
	controller := &Controller{
		Config:    cfg,
		client:    client.New(cfg.Kubeconfig),
		informers: map[reflect.Type]cache.SharedIndexInformer{},
		logger:    logrus.WithField("pkg", "controller"),
		queue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "notebook"),
	}

	indexer := cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}

	informer := jupyterv1informers.NewNotebookInformer(controller.client.VersionedInterface(), cfg.Namespace, resyncPeriod, indexer)
	controller.notebookLister = jupyterv1listers.NewNotebookLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&jupyterv1.Notebook{})] = informer

	informer = appsv1informers.NewStatefulSetInformer(controller.client, cfg.Namespace, resyncPeriod, indexer)
	controller.statefulSetLister = appsv1listers.NewStatefulSetLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&appsv1.StatefulSet{})] = informer

	informer = v1informers.NewSecretInformer(controller.client, cfg.Namespace, resyncPeriod, indexer)
	controller.secretLister = v1listers.NewSecretLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&v1.Secret{})] = informer

	informer = v1informers.NewServiceInformer(controller.client, cfg.Namespace, resyncPeriod, indexer)
	controller.serviceLister = v1listers.NewServiceLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&v1.Service{})] = informer

	informer = v1beta1informers.NewIngressInformer(controller.client, cfg.Namespace, resyncPeriod, indexer)
	controller.ingressLister = v1beta1listers.NewIngressLister(informer.GetIndexer())
	controller.informers[reflect.TypeOf(&v1beta1.Ingress{})] = informer

	return controller
}

// Run starts the reconciliation loop of the notebook controller.
func (c *Controller) Run(stop <-chan struct{}, workers int) error {
	defer c.queue.ShutDown()

	if err := c.initCRD(stop); err != nil {
		c.logger.Errorf("failed to initialize CRD: %v", err)
		return fmt.Errorf("failed to initialize CRD: %v", err)
	}

	for _, i := range c.informers {
		go i.Run(stop)
	}

	if err := c.wait(stop); err != nil {
		return fmt.Errorf("failed to start controller: %v", err)
	}

	c.addHandlers()

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stop)
	}

	<-stop
	return nil
}

func (c *Controller) createCRD() error {
	crd := crdutils.NewCustomResourceDefinition(crdutils.Config{
		SpecDefinitionName:    "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1.Notebook",
		EnableValidation:      true,
		ResourceScope:         string(apiextensionsv1beta1.NamespaceScoped),
		Group:                 jupyterv1.GroupName,
		Kind:                  jupyterv1.NotebookKind,
		Version:               jupyterv1.SchemeGroupVersion.Version,
		Plural:                jupyterv1.NotebookPlural,
		ShortNames:            jupyterv1.NotebookShortNames,
		GetOpenAPIDefinitions: jupyterv1.GetOpenAPIDefinitions,
	})
	crd.Spec.Subresources.Scale = nil

	_, err := c.client.APIExtensionsInterface().ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err == nil {
		return nil
	}
	if apierrors.IsAlreadyExists(err) {
		c.logger.Info("CRD already exists")
		return nil
	}
	return fmt.Errorf("failed to create CRD: %v", err)
}

func (c *Controller) initCRD(stop <-chan struct{}) error {
	c.logger.Info("initializing CRD")
	// create CRD
	err := wait.PollUntil(500*time.Millisecond, func() (bool, error) {
		if err := c.createCRD(); err != nil {
			c.logger.Warnf("unable to create CRD: %v", err)
			return false, nil
		}
		return true, nil
	}, stopableTimer(60*time.Second, stop))
	if err != nil {
		return fmt.Errorf("failed creating CRD: %v", err)
	}

	// wait for CRD being established
	err = wait.PollUntil(500*time.Millisecond, func() (bool, error) {
		crd, err := c.client.APIExtensionsInterface().ApiextensionsV1beta1().CustomResourceDefinitions().Get(jupyterv1.NotebookName, metav1.GetOptions{})
		if err != nil {
			c.logger.Warnf("failed to get CRD: %v", err)
			return false, nil
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
	}, stopableTimer(60*time.Second, stop))
	if err != nil {
		return fmt.Errorf("failed waiting for CRD to be created: %v", err)
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

func (c *Controller) wait(stop <-chan struct{}) error {
	ok := true
	for it := range c.informers {
		if !cache.WaitForCacheSync(stop, c.informers[it].HasSynced) {
			c.logger.Errorf("failed to sync %q cache", it)
			ok = false
		} else {
			c.logger.Debugf("successfully synced %q cache", it)
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
	if err == nil {
		c.queue.Forget(key)
		return true
	}
	utilruntime.HandleError(fmt.Errorf("syncing %q failed: %v", key, err))
	c.queue.AddRateLimited(key)
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
			return err
		}
		return nil
	}
	if err != nil {
		c.logger.Debugf("failed to list notebook: %v", err)
		return err
	}

	n := notebook.DeepCopy()

	if err := n.Validate(); err != nil {
		if phaseErr := c.setNotebookPhase(n, jupyterv1.NotebookPhaseFailed); phaseErr != nil {
			c.logger.Warnf("failed to set notebook phase for %s: %v", key, phaseErr)
		}
		return err
	}

	n.SetDefaults()
	if n.Kind == "" {
		n.Kind = jupyterv1.NotebookKind
		n.APIVersion = jupyterv1.SchemeGroupVersion.String()
	}
	specDiff := deep.Equal(n.Spec, notebook.Spec)
	if specDiff != nil {
		fmt.Println(specDiff)
		c.logger.Debugf("setting defaults for %s", key)
		if _, err := c.client.VersionedInterface().JupyterV1().Notebooks(n.Namespace).Update(n); err != nil {
			return fmt.Errorf("failed to set defaults for %s: %v", key, err)
		}
		return nil
	}

	if n.Status.Phase != jupyterv1.NotebookPhaseRunning && n.Status.Phase != jupyterv1.NotebookPhaseFailed {
		if err = c.setNotebookPhase(n, jupyterv1.NotebookPhasePending); err != nil {
			c.logger.Warnf("failed to set notebook phase for %s: %v", key, err)
		}
	}
	if err = c.reconcileNotebookResources(n); err != nil {
		c.logger.Debugf("failed to reconcile resources for %s: %v", key, err)
		if phaseErr := c.setNotebookPhase(n, jupyterv1.NotebookPhaseFailed); phaseErr != nil {
			c.logger.Warnf("failed to set notebook phase for %s: %v", key, phaseErr)
		}
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

func stopableTimer(d time.Duration, stop <-chan struct{}) <-chan struct{} {
	t := time.NewTimer(d)
	c := make(chan struct{})

	go func() {
		defer close(c)
		select {
		case <-t.C:
			return
		case <-stop:
			t.Stop()
			return
		}
	}()

	return c
}
