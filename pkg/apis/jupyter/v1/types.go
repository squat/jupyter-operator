package v1

import (
	"encoding/json"
	"errors"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// NotebookKind is the API kind for the notebook resource.
	NotebookKind = "Notebook"
	// NotebookPlural is the plural name for the notebook resource.
	NotebookPlural = "notebooks"
)

// NotebookShortNames are convenient shortnames for the notebook resource.
var NotebookShortNames = []string{"nb", "notebook"}

// Notebook is a Jupyter notebook instance that is run as a pod.
// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
type Notebook struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object’s metadata. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#metadata
	// +k8s:openapi-gen=false
	metav1.ObjectMeta `json:"metadata"`
	// Specification of the desired behavior of the Jupyter Notebook. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#spec-and-status
	Spec NotebookSpec `json:"spec"`
	// Most recent observed status of the Jupyter Notebook. Read-only. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/api-conventions.md#spec-and-status
	Status NotebookStatus `json:"status,omitempty"`
}

// NotebookSpec is the description and configuration of a notebook.
// +k8s:openapi-gen=true
type NotebookSpec struct {
	// Flavor is the type of official Jupyter image to use
	// for the notebook pod. It is the "X" in "jupyter/X-notebook".
	// Defaults to "minimal". For descriptions of valid images, see:
	// https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html.
	// +optional
	Flavor *NotebookFlavor `json:"flavor,omitempty"`
	// Whether or not to add a GPU resource to the notebook pod.
	GPU bool `json:"gpu"`
	// Host to set on the notebook ingress resource.
	// If no host is provided, no ingress will be created.
	// +optional
	Host *string `json:"host,omitempty"`
	// Ingress backend to use for the notebook ingress resource.
	// Defaults to the notebook service created by the operator.
	// +optional
	Ingress *extensionsv1beta1.IngressBackend `json:"ingress,omitempty"`
	// Owner is the user who owns the notebook.
	Owner string `json:"owner"`
	// Password to use to access the notebook.
	// +optional
	Password *string `json:"password,omitempty"`
	// TLS strategy to use. Must be "false", "acme", or "self-signed".
	// Defaults to "self-signed".
	// +optional
	TLS *NotebookTLS `json:"tls,omitempty"`
	// Users is a list of users who should have access to the notebook.
	Users []string `json:"users,omitempty"`
}

// NotebookPhase is a label for the condition of a notebook at the current time.
type NotebookPhase string

const (
	// NotebookPhasePending means that the notebook has been accepted and validated
	// but not all the resources are ready.
	NotebookPhasePending NotebookPhase = "Pending"
	// NotebookPhaseRunning means that all the notebook resources are ready.
	NotebookPhaseRunning NotebookPhase = "Running"
	// NotebookPhaseUnknown means that for some reason the state of the notebook
	// could not be determined.
	NotebookPhaseUnknown NotebookPhase = "Unknown"
	// NotebookPhaseFailed means that the system was unable to create at least one
	// of the notebook's resources.
	NotebookPhaseFailed NotebookPhase = "Failed"
)

// NotebookTLS defines the notebook's TLS strategy.
type NotebookTLS string

const (
	// NotebookTLSSelfSigned means that the notebook server will serve HTTP over TLS using
	// certificates signed by the controller. Ingress to the notebook will terminate TLS at
	// notebook and not at the ingress controller.
	// This is the default TLS strategy.
	NotebookTLSSelfSigned NotebookTLS = "self-signed"
	// NotebookTLSAcme means that the notebook server will serve HTTP over TLS using
	// certificates signed by the controller. The ingress resource will be annotated with
	// kubernetes.io/tls-acme=true and the ingress controller will terminate TLS
	// using certificates generated via LetsEncrypt. This requires kube-lego or an equivalent.
	NotebookTLSAcme NotebookTLS = "acme"
	// NotebookTLSNone means that the notebook server will serve plain HTTP with no encryption.
	// The ingress resource will not have a TLS entry so the notebook will only be accessible over HTTP.
	NotebookTLSNone NotebookTLS = "none"
)

// NotebookFlavor is the type of official Jupyter image to use
// It is the "X" in "jupyter/X-notebook".
// For descriptions of valid images, see:
// https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html.
type NotebookFlavor string

const (
	// NotebookFlavorBase is the most basic type of notebook.
	// See: https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html#jupyter-base-notebook.
	NotebookFlavorBase NotebookFlavor = "base"
	// NotebookFlavorMinimal adds command line tools useful when working in Jupyter applications.
	// See: https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html#jupyter-minimal-notebook.
	NotebookFlavorMinimal NotebookFlavor = "minimal"
	// NotebookFlavorR includes popular packages from the R ecosystem.
	// See: https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html#jupyter-minimal-notebook.
	NotebookFlavorR NotebookFlavor = "r"
	// NotebookFlavorScipy includes popular packages from the scientific Python ecosystem.
	// See: https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html#jupyter-scipy-notebook
	NotebookFlavorScipy NotebookFlavor = "scipy"
	// NotebookTensorFlow includes popular Python deep learning libraries.
	// See: https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html#jupyter-tensorflow-notebook
	NotebookFlavorTensorFlow NotebookFlavor = "tensorflow"
	// NotebookFlavorDataScience includes includes libraries for data analysis from the Julia, Python, and R communities.
	// See: https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html#jupyter-datascience-notebook
	NotebookFlavorDataScience NotebookFlavor = "datascience"
	// NotebookFlavorPyspark includes Python support for Apache Spark, optionally on Mesos.
	// See: https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html#jupyter-pyspark-notebook
	NotebookFlavorPyspark NotebookFlavor = "pyspark"
	// NotebookFlavorAllSpark includes Python, R, and Scala support for Apache Spark, optionally on Mesos.
	// See: https://jupyter-docker-stacks.readthedocs.io/en/latest/using/selecting.html#jupyter-all-spark-notebook
	NotebookFlavorAllSpark NotebookFlavor = "all-spark"
)

// NotebookStatus describes the current status of the notebook resource.
// +k8s:openapi-gen=true
type NotebookStatus struct {
	// Phase contains the current NotebookPhase of the notebook.
	Phase NotebookPhase `json:"phase"`
}

// NotebookName is the notebook resource's FQDN.
var NotebookName = NotebookPlural + "." + GroupName

// AsOwner creates a new owner reference for the notebook to apply to dependent resource.
func (n *Notebook) AsOwner() metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion:         n.APIVersion,
		Kind:               n.Kind,
		Name:               n.Name,
		UID:                n.UID,
		BlockOwnerDeletion: &trueVar,
		Controller:         &trueVar,
	}
}

// Copy creates a deep copy of the notebook.
func (n *Notebook) Copy() *Notebook {
	new := Notebook{}
	b, err := json.Marshal(*n)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(b, &new)
	if err != nil {
		panic(err)
	}
	return &new
}

// Validate ensures that all the fields of a notebook's spec are valid.
func (n *Notebook) Validate() error {
	if n.Spec.Host != nil {
		if errs := validation.IsDNS1123Subdomain(*n.Spec.Host); errs != nil {
			return errors.New("host is required and must be a valid DNS-1123 subdomain")
		}
	}
	if n.Spec.Ingress != nil {
		if n.Spec.Ingress.ServiceName == "" || n.Spec.Ingress.ServicePort.String() == "" {
			return errors.New("ingress service name and port must be both defined or both undefined")
		}
	}
	if n.Spec.Owner == "" {
		return errors.New("owner must be a valid username")
	}
	for _, user := range n.Spec.Users {
		if user == "" {
			return errors.New("users must be a list of valid usernames")
		}
	}
	return nil
}

// SetDefaults applies default values to the receiver Notebook.
func (n *Notebook) SetDefaults() bool {
	var changed bool
	defaultFlavor := NotebookFlavorMinimal
	defaultTLS := NotebookTLSSelfSigned
	emptyString := ""
	if n.Spec.Flavor == nil {
		n.Spec.Flavor = &defaultFlavor
		changed = true
	}
	if n.Spec.Host == nil {
		n.Spec.Host = &emptyString
		changed = true
	}
	if n.Spec.Password == nil {
		n.Spec.Password = &emptyString
		changed = true
	}
	if n.Spec.TLS == nil {
		n.Spec.TLS = &defaultTLS
		changed = true
	}
	if n.Spec.Users == nil {
		n.Spec.Users = []string{}
		changed = true
	}
	return changed
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NotebookList is a list of notebooks.
type NotebookList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	// List of notebooks.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md
	Items []Notebook `json:"items"`
}
