package asset

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"text/template"

	"github.com/ghodss/yaml"

	"github.com/kubernetes-incubator/bootkube/pkg/asset/internal"
)

const (
	// The name of the k8s service that selects self-hosted etcd pods
	EtcdServiceName = "etcd-service"

	SecretEtcdPeer   = "etcd-peer-tls"
	SecretEtcdServer = "etcd-server-tls"
	SecretEtcdClient = "etcd-client-tls"

	NetworkFlannel = "flannel"
	NetworkCalico  = "experimental-calico"
	NetworkCanal   = "experimental-canal"

	secretNamespace     = "kube-system"
	secretAPIServerName = "kube-apiserver"
	secretCMName        = "kube-controller-manager"
)

type staticConfig struct {
	Images ImageVersions
}

func newStaticAssets(imageVersions ImageVersions) Assets {
	conf := staticConfig{Images: imageVersions}
	assets := Assets{
		MustCreateAssetFromTemplate(AssetPathScheduler, internal.SchedulerTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathSchedulerDisruption, internal.SchedulerDisruptionTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathControllerManagerDisruption, internal.ControllerManagerDisruptionTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathKubeDNSDeployment, internal.DNSDeploymentTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathCheckpointer, internal.CheckpointerTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathKubeSystemSARoleBinding, internal.KubeSystemSARoleBindingTemplate, conf),
	}
	return assets
}

func newDynamicAssets(conf Config) Assets {
	assets := Assets{
		MustCreateAssetFromTemplate(AssetPathControllerManager, internal.ControllerManagerTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathAPIServer, internal.APIServerTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathProxy, internal.ProxyTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathKubeDNSSvc, internal.DNSSvcTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathBootstrapAPIServer, internal.BootstrapAPIServerTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathBootstrapControllerManager, internal.BootstrapControllerManagerTemplate, conf),
		MustCreateAssetFromTemplate(AssetPathBootstrapScheduler, internal.BootstrapSchedulerTemplate, conf),
	}
	if conf.SelfHostKubelet {
		assets = append(assets, MustCreateAssetFromTemplate(AssetPathKubelet, internal.KubeletTemplate, conf))
	}
	if conf.SelfHostedEtcd {
		conf.EtcdServiceName = EtcdServiceName
		assets = append(assets,
			MustCreateAssetFromTemplate(AssetPathEtcdOperator, internal.EtcdOperatorTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathEtcdSvc, internal.EtcdSvcTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathKenc, internal.KencTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathBootstrapEtcd, internal.BootstrapEtcdTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathBootstrapEtcdService, internal.BootstrapEtcdSvcTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathMigrateEtcdCluster, internal.EtcdCRDTemplate, conf))
	}
	switch conf.NetworkProvider {
	case NetworkFlannel:
		assets = append(assets,
			MustCreateAssetFromTemplate(AssetPathKubeFlannelCfg, internal.KubeFlannelCfgTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathKubeFlannel, internal.KubeFlannelTemplate, conf),
		)
	case NetworkCalico:
		assets = append(assets,
			MustCreateAssetFromTemplate(AssetPathCalicoCfg, internal.CalicoCfgTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoRole, internal.CalicoRoleTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoRoleBinding, internal.CalicoRoleBindingTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoSA, internal.CalicoServiceAccountTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalico, internal.CalicoNodeTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoBGPConfigsCRD, internal.CalicoBGPConfigsCRD, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoFelixConfigsCRD, internal.CalicoFelixConfigsCRD, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoNetworkPoliciesCRD, internal.CalicoNetworkPoliciesCRD, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoIPPoolsCRD, internal.CalicoIPPoolsCRD, conf))
	case NetworkCanal:
		assets = append(assets,
			MustCreateAssetFromTemplate(AssetPathCalicoCfg, internal.CalicoCfgTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoRole, internal.CalicoRoleTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoRoleBinding, internal.CalicoRoleBindingTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoSA, internal.CalicoServiceAccountTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoPolicyOnly, internal.CalicoPolicyOnlyTemplate, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoBGPConfigsCRD, internal.CalicoBGPConfigsCRD, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoFelixConfigsCRD, internal.CalicoFelixConfigsCRD, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoNetworkPoliciesCRD, internal.CalicoNetworkPoliciesCRD, conf),
			MustCreateAssetFromTemplate(AssetPathCalicoIPPoolsCRD, internal.CalicoIPPoolsCRD, conf))
	}
	return assets
}

func newKubeConfigAsset(assets Assets, conf Config) (Asset, error) {
	caCert, err := assets.Get(AssetPathCACert)
	if err != nil {
		return Asset{}, err
	}

	kubeletCert, err := assets.Get(AssetPathKubeletCert)
	if err != nil {
		return Asset{}, err
	}

	kubeletKey, err := assets.Get(AssetPathKubeletKey)
	if err != nil {
		return Asset{}, err
	}

	type templateCfg struct {
		Server      string
		CACert      string
		KubeletCert string
		KubeletKey  string
	}

	return assetFromTemplate(AssetPathKubeConfig, internal.KubeConfigTemplate, templateCfg{
		Server:      conf.APIServers[0].String(),
		CACert:      base64.StdEncoding.EncodeToString(caCert.Data),
		KubeletCert: base64.StdEncoding.EncodeToString(kubeletCert.Data),
		KubeletKey:  base64.StdEncoding.EncodeToString(kubeletKey.Data),
	})
}

func newSelfHostedEtcdSecretAssets(assets Assets) (Assets, error) {
	var res Assets

	secretYAML, err := secretFromAssets(SecretEtcdPeer, secretNamespace, []string{
		AssetPathEtcdPeerCA,
		AssetPathEtcdPeerCert,
		AssetPathEtcdPeerKey,
	}, assets)
	if err != nil {
		return nil, err
	}
	res = append(res, Asset{Name: AssetPathEtcdPeerSecret, Data: secretYAML})

	secretYAML, err = secretFromAssets(SecretEtcdServer, secretNamespace, []string{
		AssetPathEtcdServerCA,
		AssetPathEtcdServerCert,
		AssetPathEtcdServerKey,
	}, assets)
	if err != nil {
		return nil, err
	}
	res = append(res, Asset{Name: AssetPathEtcdServerSecret, Data: secretYAML})

	secretYAML, err = secretFromAssets(SecretEtcdClient, secretNamespace, []string{
		AssetPathEtcdClientCA,
		AssetPathEtcdClientCert,
		AssetPathEtcdClientKey,
	}, assets)
	if err != nil {
		return nil, err
	}
	res = append(res, Asset{Name: AssetPathEtcdClientSecret, Data: secretYAML})

	return res, nil
}

func newAPIServerSecretAsset(assets Assets, etcdUseTLS bool) (Asset, error) {
	secretAssets := []string{
		AssetPathAPIServerKey,
		AssetPathAPIServerCert,
		AssetPathServiceAccountPubKey,
		AssetPathCACert,
	}
	if etcdUseTLS {
		secretAssets = append(secretAssets, []string{
			AssetPathEtcdClientCA,
			AssetPathEtcdClientCert,
			AssetPathEtcdClientKey,
		}...)
	}

	secretYAML, err := secretFromAssets(secretAPIServerName, secretNamespace, secretAssets, assets)
	if err != nil {
		return Asset{}, err
	}

	return Asset{Name: AssetPathAPIServerSecret, Data: secretYAML}, nil
}

func newControllerManagerSecretAsset(assets Assets) (Asset, error) {
	secretAssets := []string{
		AssetPathServiceAccountPrivKey,
		AssetPathCACert, //TODO(aaron): do we want this also distributed as secret? or expect available on host?
	}

	secretYAML, err := secretFromAssets(secretCMName, secretNamespace, secretAssets, assets)
	if err != nil {
		return Asset{}, err
	}

	return Asset{Name: AssetPathControllerManagerSecret, Data: secretYAML}, nil
}

// TODO(aaron): use actual secret object (need to wrap in apiversion/type)
type secret struct {
	ApiVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   map[string]string `json:"metadata"`
	Type       string            `json:"type"`
	Data       map[string]string `json:"data"`
}

func secretFromAssets(name, namespace string, assetNames []string, assets Assets) ([]byte, error) {
	data := make(map[string]string)
	for _, an := range assetNames {
		a, err := assets.Get(an)
		if err != nil {
			return []byte{}, err
		}
		data[filepath.Base(a.Name)] = base64.StdEncoding.EncodeToString(a.Data)
	}
	return yaml.Marshal(secret{
		ApiVersion: "v1",
		Kind:       "Secret",
		Type:       "Opaque",
		Metadata: map[string]string{
			"name":      name,
			"namespace": namespace,
		},
		Data: data,
	})
}

func MustCreateAssetFromTemplate(name string, template []byte, data interface{}) Asset {
	a, err := assetFromTemplate(name, template, data)
	if err != nil {
		panic(err)
	}
	return a
}

func assetFromTemplate(name string, tb []byte, data interface{}) (Asset, error) {
	tmpl, err := template.New(name).Parse(string(tb))
	if err != nil {
		return Asset{}, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return Asset{}, err
	}
	return Asset{Name: name, Data: buf.Bytes()}, nil
}
