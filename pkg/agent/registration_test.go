package agent

import (
	"reflect"
	"testing"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

func TestKubeClientRegistrationAPI(t *testing.T) {
	cases := []struct {
		name string
		reg  *KubeClientRegistration
		want addonv1beta1.RegistrationConfig
	}{
		{
			name: "empty user and groups omits kubeClient config",
			reg:  &KubeClientRegistration{},
			want: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
			},
		},
		{
			name: "user only sets kubeClient config",
			reg:  &KubeClientRegistration{User: "addon-user"},
			want: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User: "addon-user",
						},
					},
				},
			},
		},
		{
			name: "groups only sets kubeClient config",
			reg:  &KubeClientRegistration{Groups: []string{"group-a", "group-b"}},
			want: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							Groups: []string{"group-a", "group-b"},
						},
					},
				},
			},
		},
		{
			name: "user and groups both set kubeClient config",
			reg:  &KubeClientRegistration{User: "addon-user", Groups: []string{"group-a"}},
			want: addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   "addon-user",
							Groups: []string{"group-a"},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.reg.RegistrationAPI()
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestCustomSignerRegistrationAPI(t *testing.T) {
	reg := &CustomSignerRegistration{
		SignerName:        "example.io/my-signer",
		User:              "addon-user",
		Groups:            []string{"group-a"},
		OrganizationUnits: []string{"ou-a", "ou-b"},
	}

	want := addonv1beta1.RegistrationConfig{
		Type: addonv1beta1.CustomSigner,
		CustomSigner: &addonv1beta1.CustomSignerConfig{
			SignerName: "example.io/my-signer",
			Subject: addonv1beta1.Subject{
				BaseSubject: addonv1beta1.BaseSubject{
					User:   "addon-user",
					Groups: []string{"group-a"},
				},
				OrganizationUnits: []string{"ou-a", "ou-b"},
			},
		},
	}

	got := reg.RegistrationAPI()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCustomSignerRegistrationAPIWithoutOptionalFields(t *testing.T) {
	reg := &CustomSignerRegistration{SignerName: "example.io/my-signer"}

	want := addonv1beta1.RegistrationConfig{
		Type: addonv1beta1.CustomSigner,
		CustomSigner: &addonv1beta1.CustomSignerConfig{
			SignerName: "example.io/my-signer",
			Subject:    addonv1beta1.Subject{},
		},
	}

	got := reg.RegistrationAPI()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
