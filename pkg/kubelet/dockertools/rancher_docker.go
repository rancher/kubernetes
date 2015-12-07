package dockertools

import (
	"fmt"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/golang/glog"
	"github.com/hashicorp/golang-lru"
	rancher "github.com/rancher/go-rancher/client"
)

type RancherDockerClient struct {
	DockerInterface
	Rancher *rancher.RancherClient
	cache   *lru.Cache
}

func NewRancherClient(docker DockerInterface, rancher *rancher.RancherClient) (*RancherDockerClient, error) {
	c, err := lru.New(256)
	if err != nil {
		return nil, err
	}

	return &RancherDockerClient{
		DockerInterface: docker,
		Rancher:         rancher,
		cache:           c,
	}, nil
}

func isPodContainer(config *docker.Config) bool {
	return config.Image == PodInfraContainerImage
}

func (r *RancherDockerClient) CreateContainer(createOpts docker.CreateContainerOptions) (*docker.Container, error) {
	if createOpts.Config.Labels == nil {
		createOpts.Config.Labels = map[string]string{}
	}

	podContainer := isPodContainer(createOpts.Config)
	if podContainer {
		createOpts.Config.Labels["io.rancher.container.network"] = "true"
		createOpts.Config.Labels["io.rancher.service.launch.config"] = "io.rancher.service.primary.launch.config"
	}

	displayName := r.parseDisplayName(createOpts.Name, podContainer)
	if displayName != "" {
		createOpts.Config.Labels["io.rancher.container.display_name"] = displayName
	}

	return r.DockerInterface.CreateContainer(createOpts)
}

func (r *RancherDockerClient) InspectContainer(id string) (*docker.Container, error) {
	inspect, err := r.DockerInterface.InspectContainer(id)
	if err != nil {
		return nil, err
	}

	if inspect.State.Running && isPodContainer(inspect.Config) {
		return inspect, r.trySetIp(inspect)
	}

	return inspect, err
}

func (r *RancherDockerClient) trySetIp(container *docker.Container) error {
	for i := 0; i < 30; i++ {
		worked, err := r.setIp(container)
		if err != nil {
			glog.Errorf("Failed to find IP for %s: %v\n", container.ID, err)
		} else if worked {
			return nil
		}
		glog.Infof("Waiting to find IP for %s", container.ID)
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Failed to find IP for %s", container.ID)
}

func (r *RancherDockerClient) getIp(container *docker.Container) (string, error) {
	if val, ok := r.cache.Get(container.ID); ok {
		if ip, ok := val.(string); ok {
			return ip, nil
		}
	}

	containers, err := r.Rancher.Container.List(&rancher.ListOpts{
		Filters: map[string]interface{}{
			"externalId":   container.ID,
			"removed_null": "",
		},
	})
	if err != nil {
		return "", err
	}

	if len(containers.Data) == 0 {
		return "", nil
	}

	rancherContainer := containers.Data[0]

	if rancherContainer.PrimaryIpAddress != "" {
		glog.Infof("Found IP %s for container %s", rancherContainer.PrimaryIpAddress, container.ID)
		r.cache.Add(container.ID, rancherContainer.PrimaryIpAddress)
	}
	return rancherContainer.PrimaryIpAddress, nil
}

func (r *RancherDockerClient) setIp(container *docker.Container) (bool, error) {
	ip, err := r.getIp(container)
	if ip != "" && err == nil {
		container.NetworkSettings.IPAddress = ip
		return true, nil
	}

	return false, err
}

func (r *RancherDockerClient) parseDisplayName(fullName string, podContainer bool) string {
	parts := strings.SplitN(fullName, "_", 4)
	if len(parts) == 4 {
		if podContainer {
			return parts[2]
		} else {
			parts = strings.Split(parts[1], ".")
			if len(parts) > 0 {
				return parts[0]
			}
		}
	}
	return ""
}
