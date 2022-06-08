/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

<<<<<<< HEAD:vendor/k8s.io/apiserver/pkg/apis/apiserver/v1alpha1/conversion.go
package v1alpha1

import (
	conversion "k8s.io/apimachinery/pkg/conversion"
	apiserver "k8s.io/apiserver/pkg/apis/apiserver"
)

func Convert_v1alpha1_EgressSelection_To_apiserver_EgressSelection(in *EgressSelection, out *apiserver.EgressSelection, s conversion.Scope) error {
	if err := autoConvert_v1alpha1_EgressSelection_To_apiserver_EgressSelection(in, out, s); err != nil {
		return err
	}
	if out.Name == "master" {
		out.Name = "controlplane"
	}
	return nil
}
=======
package klog

import (
	"github.com/go-logr/logr"
)

// The reason for providing these aliases is to allow code to work with logr
// without directly importing it.

// Logger in this package is exactly the same as logr.Logger.
type Logger = logr.Logger

// LogSink in this package is exactly the same as logr.LogSink.
type LogSink = logr.LogSink

// Runtimeinfo in this package is exactly the same as logr.RuntimeInfo.
type RuntimeInfo = logr.RuntimeInfo

var (
	// New is an alias for logr.New.
	New = logr.New
)
>>>>>>> Update condition when call manifest failed:vendor/k8s.io/klog/v2/imports.go
