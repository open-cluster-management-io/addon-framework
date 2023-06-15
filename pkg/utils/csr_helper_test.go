package utils

import (
	"bytes"
	"crypto/x509/pkix"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	"github.com/stretchr/testify/assert"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	fakeaddon "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func newCSRWithSigner(signer, commonName, clusterName string, orgs ...string) *certificatesv1.CertificateSigningRequest {
	csr := newCSR(commonName, clusterName, orgs...)
	csr.Spec.SignerName = signer
	return csr
}

func newCSR(commonName string, clusterName string, orgs ...string) *certificatesv1.CertificateSigningRequest {
	clientKey, _ := keyutil.MakeEllipticPrivateKeyPEM()
	privateKey, _ := keyutil.ParsePrivateKeyPEM(clientKey)

	request, _ := certutil.MakeCSR(privateKey, &pkix.Name{CommonName: commonName, Organization: orgs}, []string{"test.localhost"}, nil)

	return &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Usages: []certificatesv1.KeyUsage{
				certificatesv1.UsageClientAuth,
			},
			Username: "system:open-cluster-management:" + clusterName,
			Request:  request,
		},
	}
}

func newCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func newAddon(name, namespace string) *addonapiv1alpha1.ManagedClusterAddOn {
	return &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func TestDefaultSigner(t *testing.T) {
	caConfig, err := crypto.MakeSelfSignedCAConfig("test", 10)
	if err != nil {
		t.Errorf("Failed to generate self signed CA config: %v", err)
	}

	ca, key, err := caConfig.GetPEMBytes()
	if err != nil {
		t.Errorf("Failed to get ca cert/key: %v", err)
	}

	signer := DefaultSignerWithExpiry(key, ca, 24*time.Hour)

	cert := signer(newCSR("test", "cluster1"))
	if cert == nil {
		t.Errorf("Expect cert to be signed")
	}

	certs, err := crypto.CertsFromPEM(cert)
	if err != nil {
		t.Errorf("Failed to parse cert: %v", err)
	}

	if len(certs) != 1 {
		t.Errorf("Expect 1 cert signed but got %d", len(certs))
	}

	if certs[0].Issuer.CommonName != "test" {
		t.Errorf("CommonName is not correct")
	}
}

func TestDefaultCSRApprover(t *testing.T) {
	cases := []struct {
		name     string
		csr      *certificatesv1.CertificateSigningRequest
		cluster  *clusterv1.ManagedCluster
		addon    *addonapiv1alpha1.ManagedClusterAddOn
		approved bool
	}{
		{
			name:     "approve csr",
			csr:      newCSR(agent.DefaultUser("cluster1", "addon1", "test"), "cluster1", agent.DefaultGroups("cluster1", "addon1")...),
			cluster:  newCluster("cluster1"),
			addon:    newAddon("addon1", "cluster1"),
			approved: true,
		},
		{
			name:     "requester is not correct",
			csr:      newCSR(agent.DefaultUser("cluster1", "addon1", "test"), "cluster2", agent.DefaultGroups("cluster1", "addon1")...),
			cluster:  newCluster("cluster1"),
			addon:    newAddon("addon1", "cluster1"),
			approved: false,
		},
		{
			name:     "common name is not correct",
			csr:      newCSR("test", "cluster1", agent.DefaultGroups("cluster1", "addon1")...),
			cluster:  newCluster("cluster1"),
			addon:    newAddon("addon1", "cluster1"),
			approved: false,
		},
		{
			name:     "group is not correct",
			csr:      newCSR(agent.DefaultUser("cluster1", "addon1", "test"), "cluster1", "group1"),
			cluster:  newCluster("cluster1"),
			addon:    newAddon("addon1", "cluster1"),
			approved: false,
		},
	}

	approver := DefaultCSRApprover("test")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			approved := approver(c.cluster, c.addon, c.csr)
			if approved != c.approved {
				t.Errorf("Expected approve is %t, but got %t", c.approved, approved)
			}
		})
	}
}

func TestIsCSRSupported(t *testing.T) {
	cases := []struct {
		apiResources    []*metav1.APIResourceList
		expectedV1      bool
		expectedV1beta1 bool
		expectedError   error
	}{
		{
			apiResources: []*metav1.APIResourceList{
				{
					GroupVersion: certificatesv1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{
							Name: "certificatesigningrequests",
							Kind: "CertificateSigningRequest",
						},
					},
				},
				{
					GroupVersion: certificatesv1beta1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{
							Name: "certificatesigningrequests",
							Kind: "CertificateSigningRequest",
						},
					},
				},
			},
			expectedV1:      true,
			expectedV1beta1: true,
		},
		{
			apiResources: []*metav1.APIResourceList{
				{
					GroupVersion: certificatesv1beta1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{
							Name: "certificatesigningrequests",
							Kind: "CertificateSigningRequest",
						},
					},
				},
			},
			expectedV1:      false,
			expectedV1beta1: true,
		},
	}
	for _, c := range cases {
		fakeClient := fake.NewSimpleClientset()
		fakeClient.Resources = c.apiResources
		v1Supported, v1beta1Supported, err := IsCSRSupported(fakeClient)
		assert.Equal(t, c.expectedV1, v1Supported)
		assert.Equal(t, c.expectedV1beta1, v1beta1Supported)
		assert.Equal(t, c.expectedError, err)
	}
}

func TestTemplateCSRConfigurationsFunc(t *testing.T) {
	cases := []struct {
		name            string
		agentName       string
		cluster         *clusterv1.ManagedCluster
		addon           *addonapiv1alpha1.ManagedClusterAddOn
		template        *addonapiv1alpha1.AddOnTemplate
		expectedConfigs []addonapiv1alpha1.RegistrationConfig
	}{
		{
			name:            "empty",
			agentName:       "agent1",
			cluster:         NewFakeManagedCluster("cluster1"),
			addon:           NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "", ""),
			template:        NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{}),
			expectedConfigs: []addonapiv1alpha1.RegistrationConfig{},
		},
		{
			name:      "kubeclient",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "ClusterRole",
									Name:     "test",
								},
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
								},
							},
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "kubernetes.io/kube-apiserver-client",
					Subject: addonapiv1alpha1.Subject{
						User: "system:open-cluster-management:cluster:cluster1:addon:addon1:agent:agent1",

						Groups: []string{
							"system:open-cluster-management:cluster:cluster1:addon:addon1",
							"system:open-cluster-management:addon:addon1",
							"system:authenticated",
						},
						OrganizationUnits: []string{},
					},
				},
			},
		},
		{
			name:      "customsigner",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Namespace: "ns1",
							Name:      "name1"},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			expectedConfigs: []addonapiv1alpha1.RegistrationConfig{
				{
					SignerName: "s1",
					Subject: addonapiv1alpha1.Subject{
						User: "u1",
						Groups: []string{
							"g1",
							"g2",
						},
						OrganizationUnits: []string{},
					},
				},
			},
		},
	}
	for _, c := range cases {
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
		if err := mcaStore.Add(c.addon); err != nil {
			t.Fatal(err)
		}
		atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
		if err := atStore.Add(c.template); err != nil {
			t.Fatal(err)
		}

		f := TemplateCSRConfigurationsFunc(c.addon.Name, c.agentName, DefaultDesiredAddonTemplateGetter(
			addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
			addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Lister(),
		))
		registrationConfigs := f(c.cluster)
		if !equality.Semantic.DeepEqual(registrationConfigs, c.expectedConfigs) {
			t.Errorf("expected registrationConfigs %v, but got %v", c.expectedConfigs, registrationConfigs)
		}
	}
}

func TestTemplateCSRApproveCheckFunc(t *testing.T) {
	cases := []struct {
		name            string
		agentName       string
		cluster         *clusterv1.ManagedCluster
		addon           *addonapiv1alpha1.ManagedClusterAddOn
		template        *addonapiv1alpha1.AddOnTemplate
		csr             *certificatesv1.CertificateSigningRequest
		expectedApprove bool
	}{
		{
			name:            "empty",
			agentName:       "agent1",
			cluster:         NewFakeManagedCluster("cluster1"),
			addon:           NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "", ""),
			template:        NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{}),
			expectedApprove: false,
		},
		{
			name:      "kubeclient",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "ClusterRole",
									Name:     "test",
								},
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
								},
							},
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: "kubernetes.io/kube-apiserver-client",
				},
			},
			expectedApprove: false, // fake csr data
		},
		{
			name:      "customsigner",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Namespace: "ns1",
							Name:      "name1"},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: "s1",
				},
			},
			expectedApprove: true,
		},
	}
	for _, c := range cases {
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
		if err := mcaStore.Add(c.addon); err != nil {
			t.Fatal(err)
		}
		atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
		if err := atStore.Add(c.template); err != nil {
			t.Fatal(err)
		}
		f := TemplateCSRApproveCheckFunc(c.addon.Name, c.agentName, DefaultDesiredAddonTemplateGetter(
			addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
			addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Lister(),
		))
		approve := f(c.cluster, c.addon, c.csr)
		if approve != c.expectedApprove {
			t.Errorf("expected approve result %v, but got %v", c.expectedApprove, approve)
		}
	}
}

func TestTemplateCSRSignFunc(t *testing.T) {
	cases := []struct {
		name         string
		agentName    string
		cluster      *clusterv1.ManagedCluster
		addon        *addonapiv1alpha1.ManagedClusterAddOn
		template     *addonapiv1alpha1.AddOnTemplate
		csr          *certificatesv1.CertificateSigningRequest
		expectedCert []byte
	}{
		{
			name:      "kubeclient",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeKubeClient,
					KubeClient: &addonapiv1alpha1.KubeClientRegistrationConfig{
						HubPermissions: []addonapiv1alpha1.HubPermissionConfig{
							{
								Type: addonapiv1alpha1.HubPermissionsBindingSingleNamespace,
								RoleRef: rbacv1.RoleRef{
									APIGroup: "rbac.authorization.k8s.io",
									Kind:     "ClusterRole",
									Name:     "test",
								},
								SingleNamespace: &addonapiv1alpha1.SingleNamespaceBindingConfig{
									Namespace: "test",
								},
							},
						},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: "kubernetes.io/kube-apiserver-client",
					Username:   "system:open-cluster-management:cluster1:adcde",
				},
			},
			expectedCert: nil,
		},
		{
			name:      "customsigner no ca secret",
			agentName: "agent1",
			cluster:   NewFakeManagedCluster("cluster1"),
			template: NewFakeAddonTemplate("template1", []addonapiv1alpha1.RegistrationSpec{
				{
					Type: addonapiv1alpha1.RegistrationTypeCustomSigner,
					CustomSigner: &addonapiv1alpha1.CustomSignerRegistrationConfig{
						SignerName: "s1",
						Subject: &addonapiv1alpha1.Subject{
							User: "u1",
							Groups: []string{
								"g1",
								"g2",
							},
							OrganizationUnits: []string{},
						},
						SigningCA: addonapiv1alpha1.SigningCARef{
							Namespace: "ns1",
							Name:      "name1"},
					},
				},
			}),
			addon: NewFakeTemplateManagedClusterAddon("addon1", "cluster1", "template1", "fakehash"),
			csr: &certificatesv1.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "csr1",
				},
				Spec: certificatesv1.CertificateSigningRequestSpec{
					SignerName: "s1",
					Username:   "system:open-cluster-management:cluster1:adcde",
				},
			},
			expectedCert: nil,
		},
	}
	for _, c := range cases {
		addonClient := fakeaddon.NewSimpleClientset(c.template, c.addon)
		hubKubeClient := fakekube.NewSimpleClientset()
		addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute)
		mcaStore := addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Informer().GetStore()
		if err := mcaStore.Add(c.addon); err != nil {
			t.Fatal(err)
		}
		atStore := addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Informer().GetStore()
		if err := atStore.Add(c.template); err != nil {
			t.Fatal(err)
		}

		f := TemplateCSRSignFunc(c.addon.Name, c.agentName, DefaultDesiredAddonTemplateGetter(
			addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
			addonInformerFactory.Addon().V1alpha1().AddOnTemplates().Lister(),
		), hubKubeClient)
		cert := f(c.csr)
		if !bytes.Equal(cert, c.expectedCert) {
			t.Errorf("expected cert %v, but got %v", c.expectedCert, cert)
		}
	}
}

func NewFakeManagedCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ManagedCluster",
			APIVersion: clusterv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1.ManagedClusterSpec{},
	}
}

func NewFakeTemplateManagedClusterAddon(name, clusterName, addonTemplateName, addonTemplateSpecHash string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := &addonapiv1alpha1.ManagedClusterAddOn{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: clusterName,
		},
		Spec:   addonapiv1alpha1.ManagedClusterAddOnSpec{},
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{},
	}

	if addonTemplateName != "" {
		addon.Status.ConfigReferences = []addonapiv1alpha1.ConfigReference{
			{
				ConfigGroupResource: addonapiv1alpha1.ConfigGroupResource{
					Group:    "addon.open-cluster-management.io",
					Resource: "addontemplates",
				},
				ConfigReferent: addonapiv1alpha1.ConfigReferent{
					Name: addonTemplateName,
				},
				DesiredConfig: &addonapiv1alpha1.ConfigSpecHash{
					ConfigReferent: addonapiv1alpha1.ConfigReferent{
						Name: addonTemplateName,
					},
					SpecHash: addonTemplateSpecHash,
				},
			},
		}
	}
	return addon
}

func NewFakeAddonTemplate(name string,
	registrationSpec []addonapiv1alpha1.RegistrationSpec) *addonapiv1alpha1.AddOnTemplate {
	return &addonapiv1alpha1.AddOnTemplate{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: addonapiv1alpha1.AddOnTemplateSpec{
			Registration: registrationSpec,
		},
	}
}
