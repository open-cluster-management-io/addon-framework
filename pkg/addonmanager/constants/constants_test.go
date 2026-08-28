package constants

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

func TestGetHostedModeInfo(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantMode    string
		wantHost    string
	}{
		{name: "no annotations", wantMode: InstallModeDefault},
		{
			name: "install mode is only a discovery request",
			annotations: map[string]string{
				addonv1beta1.InstallModeAnnotationKey: InstallModeHosted,
			},
			wantMode: InstallModeDefault,
		},
		{
			name: "resolved hosting cluster selects hosted mode",
			annotations: map[string]string{
				addonv1beta1.HostingClusterNameAnnotationKey: "hosting",
			},
			wantMode: InstallModeHosted,
			wantHost: "hosting",
		},
		{
			name: "explicit hosting cluster wins regardless of install mode",
			annotations: map[string]string{
				addonv1beta1.InstallModeAnnotationKey:        InstallModeDefault,
				addonv1beta1.HostingClusterNameAnnotationKey: "hosting",
			},
			wantMode: InstallModeHosted,
			wantHost: "hosting",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, host := GetHostedModeInfo(&addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Annotations: test.annotations},
			}, nil)
			if mode != test.wantMode || host != test.wantHost {
				t.Fatalf("GetHostedModeInfo() = (%q, %q), want (%q, %q)",
					mode, host, test.wantMode, test.wantHost)
			}
		})
	}
}
