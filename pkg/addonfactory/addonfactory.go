package addonfactory

import (
	"embed"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const AddonDefaultInstallNamespace = "open-cluster-management-agent-addon"

// AnnotationValuesName is the annotation Name of customized values
const AnnotationValuesName string = "addon.open-cluster-management.io/values"

type Values map[string]interface{}

type GetValuesFunc func(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error)

// AgentAddonFactory includes the common fields for building different agentAddon instances.
type AgentAddonFactory struct {
	scheme            *runtime.Scheme
	fs                embed.FS
	dir               string
	managementDir     string
	getValuesFuncs    []GetValuesFunc
	agentAddonOptions agent.AgentAddonOptions
	// trimCRDDescription flag is used to trim the description of CRDs in manifestWork. disabled by default.
	trimCRDDescription bool
}

// NewAgentAddonFactory builds an addonAgentFactory instance with addon name and fs.
// dir is the path prefix based on the fs path.
func NewAgentAddonFactory(addonName string, fs embed.FS, dir string) *AgentAddonFactory {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = apiextensionsv1.AddToScheme(s)
	_ = apiextensionsv1beta1.AddToScheme(s)
	return &AgentAddonFactory{
		fs:  fs,
		dir: dir,
		agentAddonOptions: agent.AgentAddonOptions{
			AddonName:       addonName,
			Registration:    nil,
			InstallStrategy: nil,
		},
		trimCRDDescription: false,
		scheme:             s,
	}
}

// WithScheme is an optional configuration, only used when the agentAddon has customized resource types.
func (f *AgentAddonFactory) WithScheme(scheme *runtime.Scheme) *AgentAddonFactory {
	f.scheme = scheme
	return f
}

// WithGetValuesFuncs adds a list of the getValues func.
// the values got from the big index Func will override the one from small index Func.
func (f *AgentAddonFactory) WithGetValuesFuncs(getValuesFuncs ...GetValuesFunc) *AgentAddonFactory {
	f.getValuesFuncs = getValuesFuncs
	return f
}

// WithInstallStrategy defines the installation strategy of the manifests prescribed by Manifests(..).
func (f *AgentAddonFactory) WithInstallStrategy(strategy *agent.InstallStrategy) *AgentAddonFactory {
	if strategy.InstallNamespace == "" {
		strategy.InstallNamespace = AddonDefaultInstallNamespace
	}
	f.agentAddonOptions.InstallStrategy = strategy

	return f
}

// WithAgentRegistrationOption defines how agent is registered to the hub cluster.
func (f *AgentAddonFactory) WithAgentRegistrationOption(option *agent.RegistrationOption) *AgentAddonFactory {
	f.agentAddonOptions.Registration = option
	return f
}

// WithTrimCRDDescription is to enable trim the description of CRDs in manifestWork.
func (f *AgentAddonFactory) WithTrimCRDDescription() *AgentAddonFactory {
	f.trimCRDDescription = true
	return f
}

// WithManagementDir is to set the management templates dir, this is required by hosted mode.
func (f *AgentAddonFactory) WithManagementDir(dir string) *AgentAddonFactory {
	f.managementDir = dir
	return f
}

// BuildHelmAgentAddon builds a helm agentAddon instance.
func (f *AgentAddonFactory) BuildHelmAgentAddon() (agent.AgentAddon, error) {
	// if f.scheme == nil {
	// 	f.scheme = runtime.NewScheme()
	// }
	// _ = scheme.AddToScheme(f.scheme)
	// _ = apiextensionsv1.AddToScheme(f.scheme)
	// _ = apiextensionsv1beta1.AddToScheme(f.scheme)

	userChart, err := loadChart(f.fs, f.dir)
	if err != nil {
		return nil, err
	}
	// TODO: validate chart

	agentAddon := newHelmAgentAddon(f, userChart)

	return agentAddon, nil
}

// BuildTemplateAgentAddon builds a template agentAddon instance.
func (f *AgentAddonFactory) BuildTemplateAgentAddon() (agent.AgentAddon, error) {
	templateFiles, err := getTemplateFiles(f.fs, f.dir)
	if err != nil {
		klog.Errorf("failed to get template files. %v", err)
		return nil, err
	}
	if len(templateFiles) == 0 {
		return nil, fmt.Errorf("there is no template files")
	}

	// if f.scheme == nil {
	// 	f.scheme = runtime.NewScheme()
	// }
	// _ = scheme.AddToScheme(f.scheme)
	// _ = apiextensionsv1.AddToScheme(f.scheme)
	// _ = apiextensionsv1beta1.AddToScheme(f.scheme)

	agentAddon := newTemplateAgentAddon(f)

	for _, file := range templateFiles {
		template, err := f.fs.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if err := agentAddon.validateTemplateData(file, template, "Default"); err != nil {
			return nil, err
		}
		agentAddon.addTemplateData(file, template)
	}
	return agentAddon, nil
}

// BuildTemplateHostedAgentAddon builds a template agentAddon in Hosted mode instance.
func (f *AgentAddonFactory) BuildTemplateHostedAgentAddon() (agent.HostedAgentAddon, error) {
	if len(f.managementDir) == 0 {
		return nil, fmt.Errorf("hosted agent addon requires managementDir, please use WithManagementDir to set it")
	}

	agentAddon := newTemplateAgentAddon(f)
	var err error
	agentAddon, err = f.buildTemplateAgentAddonInner(agentAddon, f.dir, "Hosted", agentAddon.addTemplateData)
	if err != nil {
		return nil, err
	}
	return f.buildTemplateAgentAddonInner(agentAddon, f.managementDir, "Hosted", agentAddon.addManagementTemplateData)
}

func (f *AgentAddonFactory) buildTemplateAgentAddonInner(agentAddon *TemplateAgentAddon,
	dir string, installMode string,
	addTemplateDataFunc func(file string, data []byte)) (*TemplateAgentAddon, error) {

	templateFiles, err := getTemplateFiles(f.fs, dir)
	if err != nil {
		klog.Errorf("failed to get template files. %v", err)
		return nil, err
	}
	if len(templateFiles) == 0 {
		return nil, fmt.Errorf("there is no template files")
	}

	for _, file := range templateFiles {
		template, err := f.fs.ReadFile(file)
		if err != nil {
			return nil, err
		}

		if err := agentAddon.validateTemplateData(file, template, installMode); err != nil {
			return nil, err
		}
		addTemplateDataFunc(file, template)
	}
	return agentAddon, nil
}
