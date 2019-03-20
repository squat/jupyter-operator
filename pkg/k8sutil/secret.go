package k8sutil

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"reflect"

	jupyterv1 "github.com/squat/jupyter-operator/pkg/apis/jupyter/v1"
	"github.com/squat/jupyter-operator/pkg/tls"

	"github.com/Sirupsen/logrus"
	"github.com/kubernetes-incubator/bootkube/pkg/tlsutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1client "k8s.io/client-go/kubernetes/typed/core/v1"
	v1listers "k8s.io/client-go/listers/core/v1"
)

// CalculateSecret creates a new k8s secret struct configured for the given notebook.
func CalculateSecret(n *jupyterv1.Notebook, caCert *x509.Certificate, caKey *rsa.PrivateKey) *corev1.Secret {
	labels := notebookLabels(n.Name)
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        resourceName(n.Name),
			Namespace:   n.Namespace,
			Labels:      labels,
			Annotations: map[string]string{},
		},
		Data: map[string][]byte{},
	}
	if ShouldHaveCerts(n) {
		var altNames tlsutil.AltNames
		if n.Spec.Host != nil && *n.Spec.Host != "" {
			altNames.DNSNames = append(altNames.DNSNames, *n.Spec.Host)
		}
		cert, key, err := tls.NewSignedCert(caCert, caKey, altNames, n.Name)
		if err != nil {
			panic("failed to generate certificate")
		}
		secret.Data[corev1.TLSCertKey] = tlsutil.EncodeCertificatePEM(cert)
		secret.Data[corev1.TLSPrivateKeyKey] = tlsutil.EncodePrivateKeyPEM(key)
	}
	if n.Spec.Password != nil && *n.Spec.Password != "" {
		secret.Data[notebookPasswordKey] = []byte(*n.Spec.Password)
	}
	addOwnerRefToObject(secret.GetObjectMeta(), n.AsOwner())
	return &secret
}

// CreateOrUpdateSecret will update the given secret, if it already exists, or create it if it doesn't.
// This function will adopt matching resources that are managed by the operator.
func CreateOrUpdateSecret(c v1client.SecretInterface, l v1listers.SecretLister, caCert *x509.Certificate, logger logrus.StdLogger, secret *corev1.Secret) error {
	s, err := l.Secrets(secret.Namespace).Get(secret.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err := c.Create(secret)
		return err
	}
	if !isManagedByOperator(s.Labels) {
		return fmt.Errorf("refusing to adopt existing %s: not managed by this operator", reflect.TypeOf(s))
	}
	if pem, ok := s.Data[corev1.TLSCertKey]; ok && secret.Data[corev1.TLSCertKey] != nil {
		if cert, err := tlsutil.ParsePEMEncodedCACert(pem); err == nil {
			if err = cert.CheckSignatureFrom(caCert); err == nil {
				secret.Data[corev1.TLSCertKey] = s.Data[corev1.TLSCertKey]
				secret.Data[corev1.TLSPrivateKeyKey] = s.Data[corev1.TLSPrivateKeyKey]
				logger.Print("x509 certificate was not signed by this operator's CA; recreating certificates")
			}
		}
	}
	secret.ResourceVersion = s.ResourceVersion
	_, err = c.Update(secret)
	return err
}

// DeleteSecret will delete the secret that corresponds to the given notebook.
func DeleteSecret(c v1client.SecretInterface, n *jupyterv1.Notebook) error {
	return c.Delete(resourceName(n.Name), &metav1.DeleteOptions{})
}

// ShouldHaveCerts determines whether a notebook secret should contain certificates.
func ShouldHaveCerts(n *jupyterv1.Notebook) bool {
	if n.Spec.TLS != nil && *n.Spec.TLS == jupyterv1.NotebookTLSNone {
		return false
	}
	return true
}
