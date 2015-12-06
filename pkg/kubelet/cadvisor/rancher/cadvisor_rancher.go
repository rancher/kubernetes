package rancher

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/google/cadvisor/events"
	cadvisorfs "github.com/google/cadvisor/fs"
	cadvisorapi "github.com/google/cadvisor/info/v1"
	cadvisorapiv2 "github.com/google/cadvisor/info/v2"
	"k8s.io/kubernetes/pkg/kubelet/cadvisor"
)

type cadvisorClient struct {
	*Client
	em events.EventManager
}

var _ cadvisor.Interface = new(cadvisorClient)

// Creates a cAdvisor.
func New(url string) (cadvisor.Interface, error) {
	// Create and start the cAdvisor client.
	cli, err := NewClient(url)
	if err != nil {
		return nil, err
	}

	cadvisorClient := &cadvisorClient{
		Client: cli,
		em:     events.NewEventManager(events.DefaultStoragePolicy()),
	}

	return cadvisorClient, nil
}

func (cc *cadvisorClient) Start() error {
	glog.V(2).Info("Using rancher provided cadvisor")
	return nil
}

func (cc *cadvisorClient) VersionInfo() (*cadvisorapi.VersionInfo, error) {
	attr, err := cc.Attributes()
	if err != nil {
		return nil, err
	}
	return &cadvisorapi.VersionInfo{
		KernelVersion:      attr.KernelVersion,
		ContainerOsVersion: attr.ContainerOsVersion,
		DockerVersion:      attr.DockerVersion,
		CadvisorVersion:    attr.CadvisorVersion,
		CadvisorRevision:   "unknown",
	}, nil
}

func (cc *cadvisorClient) SubcontainerInfo(name string, req *cadvisorapi.ContainerInfoRequest) (map[string]*cadvisorapi.ContainerInfo, error) {
	infos, err := cc.Client.SubcontainersInfo(name, req)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*cadvisorapi.ContainerInfo, len(infos))
	for _, info := range infos {
		result[info.Name] = info
	}
	return result, nil
}

func (cc *cadvisorClient) DockerImagesFsInfo() (cadvisorapiv2.FsInfo, error) {
	return cc.getFsInfo(cadvisorfs.LabelDockerImages)
}

func (cc *cadvisorClient) RootFsInfo() (cadvisorapiv2.FsInfo, error) {
	return cc.getFsInfo(cadvisorfs.LabelSystemRoot)
}

func (cc *cadvisorClient) getFsInfo(label string) (cadvisorapiv2.FsInfo, error) {
	res, err := cc.StorageInfo(label)
	if err != nil {
		return cadvisorapiv2.FsInfo{}, err
	}
	if len(res) == 0 {
		return cadvisorapiv2.FsInfo{}, fmt.Errorf("failed to find information for the filesystem labeled %q", label)
	}
	// TODO(vmarmol): Handle this better when a label has more than one image filesystem.
	if len(res) > 1 {
		glog.Warningf("More than one filesystem labeled %q: %#v. Only using the first one", label, res)
	}

	return res[0], nil
}

func (cc *cadvisorClient) WatchEvents(request *events.Request) (*events.EventChannel, error) {
	ec, err := cc.em.WatchEvents(request)
	if err != nil {
		return nil, err
	}

	// forward events
	go func() {
		evFw := make(chan *cadvisorapi.Event, 1)
		// request from remote
		err := cc.EventStreamingInfo(request, evFw)
		if err != nil {
			glog.Errorf("Can not get event stream from cadvisor, %e", err)
			return
		}

		for e := range evFw {
			cc.em.AddEvent(e)
		}
		// TODO(antmanler): Should we try to send request again?
		glog.V(2).Infof("Cadvisor event forwarder for %q, ended", request.ContainerName)
	}()

	return ec, nil
}
