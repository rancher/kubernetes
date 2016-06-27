/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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

// The kubelet binary is responsible for maintaining a set of containers on a particular host VM.
// It syncs data from both configuration file(s) as well as from a quorum of etcd servers.
// It then queries Docker to see what is currently running.  It synchronizes the configuration data,
// with the running set of containers by starting or stopping Docker containers.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"k8s.io/kubernetes/cmd/kubelet/app"
	"k8s.io/kubernetes/cmd/kubelet/app/options"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/version/verflag"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"k8s.io/kubernetes/pkg/cloudprovider"
	cadvisor "k8s.io/kubernetes/pkg/kubelet/cadvisor/rancher"
	"k8s.io/kubernetes/pkg/kubelet/dockertools"

	rancher "github.com/rancher/go-rancher/client"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	s := options.NewKubeletServer()
	s.AddFlags(pflag.CommandLine)

	util.InitFlags()
	util.InitLogs()
	defer util.FlushLogs()

	verflag.PrintAndExitIfRequested()

	cfg, err := injectRancherCfg(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if err := app.Run(s, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func injectRancherCfg(s *options.KubeletServer) (*app.KubeletConfig, error) {
	if strings.ToLower(s.CloudProvider) != "rancher" {
		return nil, nil
	}

	cfg, err := app.UnsecuredKubeletConfig(s)
	if err != nil {
		return nil, err
	}

	clientConfig, err := app.CreateAPIServerClientConfig(s)
	if err == nil {
		cfg.KubeClient, err = clientset.NewForConfig(clientConfig)

		// make a separate client for events
		eventClientConfig := *clientConfig
		eventClientConfig.QPS = s.EventRecordQPS
		eventClientConfig.Burst = s.EventBurst
		cfg.EventClient, err = clientset.NewForConfig(&eventClientConfig)
	}
	if err != nil && len(s.APIServerList) > 0 {
		glog.Warningf("No API client: %v", err)
	}

	cloud, err := cloudprovider.InitCloudProvider(s.CloudProvider, s.CloudConfigFile)
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Successfully initialized cloud provider: %q from the config file: %q\n", s.CloudProvider, s.CloudConfigFile)
	cfg.Cloud = cloud

	rancherClient, err := rancher.NewRancherClient(&rancher.ClientOpts{
		Url:       os.Getenv("CATTLE_URL"),
		AccessKey: os.Getenv("CATTLE_ACCESS_KEY"),
		SecretKey: os.Getenv("CATTLE_SECRET_KEY"),
	})
	if err != nil {
		return nil, err
	}

	// reusing rancher's cadvisor
	cfg.CAdvisorInterface, err = cadvisor.New("http://127.0.0.1:9344")
	if err != nil {
		glog.Error("Cannot connect to rancher's cadvisor")
		cfg.CAdvisorInterface = nil
	}

	cfg.DockerClient, err = dockertools.NewRancherClient(cfg.DockerClient, rancherClient)
	return cfg, err
}
