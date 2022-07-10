package agentdeploy

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/addon-framework/pkg/addonmanager/addontesting"
	"open-cluster-management.io/addon-framework/pkg/agent"
	fakework "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

func TestApplyWork(t *testing.T) {
	cache := newWorkCache()
	fakeWorkClient := fakework.NewSimpleClientset()
	workInformerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 10*time.Minute)
	workHealthProber := &agent.WorkHealthProber{
		ProbeFields: []agent.ProbeField{{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     "",
				Resource:  "jobs",
				Name:      "test",
				Namespace: "default",
			},
			ProbeRules: []workapiv1.FeedbackRule{{
				Type: workapiv1.WellKnownStatusType,
			}},
		}},
		HealthCheck: nil,
	}

	work, _, _ := buildManifestWorkFromObject("cluster1", "addon1", workHealthProber, []runtime.Object{addontesting.NewUnstructured("batch/v1", "Job", "default", "test")})

	if work.Spec.ManifestConfigs == nil {
		t.Errorf("failed to add work health probe fields to the work")
	}

	_, err := applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, work)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}

	addontesting.AssertActions(t, fakeWorkClient.Actions(), "create")

	// IF work is not changed, we should not update
	newWorkCopy := work.DeepCopy()
	fakeWorkClient.ClearActions()
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(work); err != nil {
		t.Errorf("failed to add work to store with err %v", err)
	}
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, newWorkCopy)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertNoActions(t, fakeWorkClient.Actions())

	// Update work spec to update it
	newWork, _, _ := buildManifestWorkFromObject("cluster1", "addon1", workHealthProber, []runtime.Object{addontesting.NewUnstructured("batch/v1", "Job", "default", "test")})
	newWork.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeOrphan}
	fakeWorkClient.ClearActions()
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertActions(t, fakeWorkClient.Actions(), "patch")

	// Do not update if generation is not changed
	work.Spec.DeleteOption = &workapiv1.DeleteOption{PropagationPolicy: workapiv1.DeletePropagationPolicyTypeForeground}
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work); err != nil {
		t.Errorf("failed to update work with err %v", err)
	}

	fakeWorkClient.ClearActions()
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work); err != nil {
		t.Errorf("failed to update work with err %v", err)
	}
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertNoActions(t, fakeWorkClient.Actions())

	// change generation will cause update
	work.Generation = 1
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work); err != nil {
		t.Errorf("failed to update work with err %v", err)
	}

	fakeWorkClient.ClearActions()
	if err := workInformerFactory.Work().V1().ManifestWorks().Informer().GetStore().Update(work); err != nil {
		t.Errorf("failed to update work with err %v", err)
	}
	_, err = applyWork(context.TODO(), fakeWorkClient, workInformerFactory.Work().V1().ManifestWorks().Lister(), cache, newWork)
	if err != nil {
		t.Errorf("failed to apply work with err %v", err)
	}
	addontesting.AssertActions(t, fakeWorkClient.Actions(), "patch")
}
