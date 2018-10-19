package client

import (
	"log"

	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"
	versioned "github.com/squat/jupyter-operator/pkg/clientset/versioned"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var _ Interface = &client{}

type client struct {
	config *rest.Config
	*kubernetes.Clientset
	extensions *apiextensions.Clientset
	rest       *rest.RESTClient
	versioned  *versioned.Clientset
}

// Interface describes the interface of a valid jupyter operator client.
type Interface interface {
	kubernetes.Interface
	APIExtensionsInterface() apiextensions.Interface
	RESTClientInterface() rest.Interface
	VersionedInterface() versioned.Interface
}

// New creates and returns a new instance of a jupyter operator client.
func New(path string) Interface {
	var config *rest.Config
	var err error

	config, err = clientcmd.BuildConfigFromFlags("", path)

	if err != nil {
		log.Fatal(err)
	}

	scheme := runtime.NewScheme()
	if err := jupyterv1.AddToScheme(scheme); err != nil {
		log.Fatal(err)
	}
	config.GroupVersion = &jupyterv1.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	return &client{config, kubernetes.NewForConfigOrDie(config), apiextensions.NewForConfigOrDie(config), newRESTClientForConfigOrDie(config), versioned.NewForConfigOrDie(config)}
}

func (c *client) APIExtensionsInterface() apiextensions.Interface {
	return c.extensions
}

func (c *client) RESTClientInterface() rest.Interface {
	return c.rest
}

func (c *client) VersionedInterface() versioned.Interface {
	return c.versioned
}

func newRESTClientForConfigOrDie(config *rest.Config) *rest.RESTClient {
	client, err := rest.RESTClientFor(config)
	if err != nil {
		log.Fatal(err)
	}
	return client
}
