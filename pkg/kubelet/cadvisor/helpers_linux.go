//go:build linux
// +build linux

/*
Copyright 2017 The Kubernetes Authors.

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

package cadvisor

import (
	"fmt"

	cadvisorfs "github.com/google/cadvisor/fs"
)

// imageFsInfoProvider knows how to translate the configured runtime
// to its file system label for images.
type imageFsInfoProvider struct {
	runtimeEndpoint string
}

// ImageFsInfoLabel returns the image fs label for the configured runtime.
// For remote runtimes, it handles additional runtimes natively understood by cAdvisor.
func (i *imageFsInfoProvider) ImageFsInfoLabel() (string, error) {
	// This is a temporary workaround to get stats for cri-dockerd from cadvisor
	// and should be removed. Related to https://github.com/Mirantis/cri-dockerd/issues/135
	if i.runtimeEndpoint == "unix://"+CriDockerdSocketv124 || i.runtimeEndpoint == CriDockerdSocketv123 ||
		i.runtimeEndpoint == CriDockerdSocketv124 || i.runtimeEndpoint == "unix://"+CriDockerdSocketv123 {
		return cadvisorfs.LabelDockerImages, nil
	}
	// This is a temporary workaround to get stats for cri-o from cadvisor
	// and should be removed.
	// Related to https://github.com/kubernetes/kubernetes/issues/51798
	if i.runtimeEndpoint == CrioSocket || i.runtimeEndpoint == "unix://"+CrioSocket {
		return cadvisorfs.LabelCrioImages, nil
	}
	return "", fmt.Errorf("no imagefs label for configured runtime")
}

// NewImageFsInfoProvider returns a provider for the specified runtime configuration.
func NewImageFsInfoProvider(runtimeEndpoint string) ImageFsInfoProvider {
	return &imageFsInfoProvider{runtimeEndpoint: runtimeEndpoint}
}
