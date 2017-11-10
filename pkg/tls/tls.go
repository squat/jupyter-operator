package tls

import (
	"crypto/rsa"
	"crypto/x509"

	"github.com/kubernetes-incubator/bootkube/pkg/tlsutil"
	"github.com/pborman/uuid"
)

const organization = "jupyter-operator"

// NewCACert generates a new CA certificate and private key.
func NewCACert() (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := tlsutil.NewPrivateKey()
	if err != nil {
		return nil, nil, err
	}
	config := tlsutil.CertConfig{
		CommonName:         "kube-ca",
		Organization:       []string{organization},
		OrganizationalUnit: []string{uuid.New()},
	}
	cert, err := tlsutil.NewSelfSignedCACertificate(config, key)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, err
}

// NewSignedCert generates a new signed certificate for a given CA.
func NewSignedCert(caCert *x509.Certificate, caKey *rsa.PrivateKey, altNames tlsutil.AltNames, commonName string) (*x509.Certificate, *rsa.PrivateKey, error) {
	key, err := tlsutil.NewPrivateKey()
	if err != nil {
		return nil, nil, err
	}
	config := tlsutil.CertConfig{
		CommonName:   commonName,
		Organization: []string{organization},
		AltNames:     altNames,
	}
	cert, err := tlsutil.NewSignedCertificate(config, key, caCert, caKey)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, err
}
