package utils

import (
	"crypto/x509/pkix"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

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

func TestUnionApprover(t *testing.T) {
	approveAll := func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
		return true
	}

	approveNone := func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
		return false
	}

	cases := []struct {
		name        string
		approveFunc []agent.CSRApproveFunc
		approved    bool
	}{
		{
			name:        "approve all",
			approveFunc: []agent.CSRApproveFunc{approveAll, approveAll},
			approved:    true,
		},
		{
			name:        "approve none",
			approveFunc: []agent.CSRApproveFunc{approveAll, approveNone},
			approved:    false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			approver := UnionCSRApprover(c.approveFunc...)
			approved := approver(
				newCluster("cluster1"),
				newAddon("addon1", "cluster1"),
				newCSR(agent.DefaultUser("cluster1", "addon1", "test"), "cluster1", "group1"),
			)
			if approved != c.approved {
				t.Errorf("Expected approve is %t, but got %t", c.approved, approved)
			}
		})
	}
}
