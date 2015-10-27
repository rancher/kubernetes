package rancher

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/rancher/go-rancher/client"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/cloudprovider"
)

const (
	providerName             = "rancher"
	lbNameFormat      string = "lb-%s"
	kubernetesEnvName string = "kubernetes-loadbalancers"
)

// CloudProvider implents Instances, Zones, and TCPLoadBalancer
type CloudProvider struct {
	client *client.RancherClient
}

// ProviderName returns the cloud provider ID.
func (r *CloudProvider) ProviderName() string {
	return providerName
}

// TCPLoadBalancer returns an implementation of TCPLoadBalancer for Rancher
func (r *CloudProvider) TCPLoadBalancer() (cloudprovider.TCPLoadBalancer, bool) {
	return r, true
}

// Zones returns an implementation of Zones for Rancher
func (r *CloudProvider) Zones() (cloudprovider.Zones, bool) {
	return r, true
}

// Instances returns an implementation of Instances for Rancher
func (r *CloudProvider) Instances() (cloudprovider.Instances, bool) {
	return r, true
}

// Clusters not supported
func (r *CloudProvider) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

// Routes not supported
func (r *CloudProvider) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

// --- TCPLoadBalancer Functions ---

type instanceCollection struct {
	Data []instanceAndHost `json:"data,omitempty"`
}

type instanceAndHost struct {
	client.Instance
	Hosts []client.Host `json:"hosts,omitempty"`
}

type hostCollection struct {
	Data []hostAndIPAddresses
}

type hostAndIPAddresses struct {
	client.Host
	IPAddresses []client.IpAddress `json:"ipAddresses,omitempty"`
}

// GetTCPLoadBalancer is an implementation of TCPLoadBalancer.GetTCPLoadBalancer
func (r *CloudProvider) GetTCPLoadBalancer(name, region string) (status *api.LoadBalancerStatus, exists bool, retErr error) {
	name = formatLBName(name)
	glog.Infof("GetTCPLoadBalancer [%s] [%s]", name, region)

	lb, err := r.getLBByName(name)
	if err != nil {
		return nil, false, err
	}

	if lb == nil {
		return &api.LoadBalancerStatus{}, false, nil
	}

	return r.toLBStatus(lb)
}

// EnsureTCPLoadBalancer is an implementation of TCPLoadBalancer.EnsureTCPLoadBalancer.
func (r *CloudProvider) EnsureTCPLoadBalancer(name, region string, loadBalancerIP net.IP, ports []*api.ServicePort, hosts []string,
	affinity api.ServiceAffinity) (*api.LoadBalancerStatus, error) {
	name = formatLBName(name)
	glog.Infof("EnsureTCPLoadBalancer [%s] [%s] [%#v] [%#v] [%s] [%s]", name, region, loadBalancerIP, ports, hosts, affinity)

	if loadBalancerIP != nil {
		// Rancher doesn't support specifying loadBalancer IP
		return nil, fmt.Errorf("loadBalancerIP cannot be specified for Rancher LoadBalancer")
	}

	if affinity != api.ServiceAffinityNone {
		// Rancher supports sticky sessions, but only when configured for HTTP/HTTPS
		return nil, fmt.Errorf("Unsupported load balancer affinity: %v", affinity)
	}

	lb, err := r.getLBByName(name)
	if err != nil {
		return nil, err
	}

	lbPorts := []string{}
	for _, port := range ports {
		if port.NodePort == 0 {
			glog.Warningf("Ignoring port without NodePort: %s", port)
		}
		lbPorts = append(lbPorts, fmt.Sprintf("%v:%v/tcp", port.Port, port.NodePort))
	}

	if lb != nil && portsChanged(lbPorts, lb.LaunchConfig.Ports) {
		// Cannot update ports on an LB, so if the ports have changed, need to recreate
		err = r.deleteLoadBalancer(lb)
		if err != nil {
			return nil, err
		}
		lb = nil
	}

	if lb == nil {
		env, err := r.getOrCreateEnvironment()
		if err != nil {
			return nil, err
		}

		lb = &client.LoadBalancerService{
			Name:          name,
			EnvironmentId: env.Id,
			LaunchConfig: client.LaunchConfig{
				Ports: lbPorts,
			},
		}

		lb, err = r.client.LoadBalancerService.Create(lb)
		if err != nil {
			return nil, fmt.Errorf("Unable to create load balancer for service %s. Error: %#v", name, err)
		}
	}

	err = r.setLBHosts(lb, hosts)
	if err != nil {
		return nil, err
	}

	actionChannel := r.waitForLBAction("activate", lb)
	lbInterface, ok := <-actionChannel
	if !ok {
		return nil, fmt.Errorf("Couldn't call activate on LB %s", lb.Name)
	}
	lb = convertLB(lbInterface)

	_, err = r.client.LoadBalancerService.ActionActivate(lb)
	if err != nil {
		return nil, fmt.Errorf("Error creating LB %s. Couldn't activate LB. Error: %#v", name, err)
	}

	lb, err = r.client.LoadBalancerService.ById(lb.Id)
	if err != nil {
		return nil, fmt.Errorf("Error creating LB %s. Couldn't reload LB to get status. Error: %#v", name, err)
	}

	status, _, err := r.toLBStatus(lb)
	if err != nil {
		return nil, err
	}

	return status, nil
}

func convertLB(intf interface{}) *client.LoadBalancerService {
	lb, ok := intf.(*client.LoadBalancerService)
	if !ok {
		panic(fmt.Sprintf("Couldn't cast to LoadBalancerService type! Interface: %#v", intf))
	}
	return lb
}

// UpdateTCPLoadBalancer is an implementation of TCPLoadBalancer.UpdateTCPLoadBalancer.
func (r *CloudProvider) UpdateTCPLoadBalancer(name, region string, hosts []string) error {
	name = formatLBName(name)
	glog.Infof("UpdateTCPLoadBalancer [%s] [%s] [%s]", name, region, hosts)
	lb, err := r.getLBByName(name)
	if err != nil {
		return err
	}

	if lb == nil {
		return fmt.Errorf("Couldn't find LB with name %s", name)
	}

	err = r.deleteLBConsumedServices(lb)
	if err != nil {
		return err
	}

	err = r.setLBHosts(lb, hosts)
	if err != nil {
		return err
	}

	return nil
}

// EnsureTCPLoadBalancerDeleted is an implementation of TCPLoadBalancer.EnsureTCPLoadBalancerDeleted.
func (r *CloudProvider) EnsureTCPLoadBalancerDeleted(name, region string) error {
	name = formatLBName(name)
	glog.Infof("EnsureTCPLoadBalancerDeleted [%s] [%s]", name, region)
	lb, err := r.getLBByName(name)
	if err != nil {
		return err
	}

	if lb == nil {
		glog.Infof("Couldn't find LB %s to delete. Nothing to do.")
		return nil
	}

	return r.deleteLoadBalancer(lb)
}

func (r *CloudProvider) getOrCreateEnvironment() (*client.Environment, error) {
	opts := client.NewListOpts()
	opts.Filters["name"] = kubernetesEnvName
	opts.Filters["removed_null"] = "1"

	envs, err := r.client.Environment.List(opts)
	if err != nil {
		return nil, fmt.Errorf("Coudln't get host by name [%s]. Error: %#v", kubernetesEnvName, err)
	}

	if len(envs.Data) >= 1 {
		return &envs.Data[0], nil
	}

	env := &client.Environment{
		Name: kubernetesEnvName,
	}

	env, err = r.client.Environment.Create(env)
	if err != nil {
		return nil, fmt.Errorf("Couldn't create environment for kubernetes LBs. Error: %#v", err)
	}
	return env, nil
}

func (r *CloudProvider) setLBHosts(lb *client.LoadBalancerService, hosts []string) error {
	serviceLinks := &client.SetLoadBalancerServiceLinksInput{}
	for _, hostname := range hosts {
		opts := client.NewListOpts()
		opts.Filters["name"] = hostname
		opts.Filters["environmentId"] = lb.EnvironmentId
		opts.Filters["removed_null"] = "1"

		exSvces, err := r.client.ExternalService.List(opts)
		if err != nil {
			return fmt.Errorf("Couldn't get external service %s for LB %s. Error: %#v.", hostname, lb.Name, err)
		}

		var exSvc *client.ExternalService
		if len(exSvces.Data) > 0 {
			exSvc = &exSvces.Data[0]
		} else {
			host, err := r.getHostByName(hostname)
			if err != nil {
				return fmt.Errorf("Couldn't create extrnal service %s for LB %s. Error: %#v", hostname, lb.Name, err)
			}

			coll := &client.IpAddressCollection{}
			err = r.client.GetLink(host.Resource, "ipAddresses", coll)
			if err != nil {
				return fmt.Errorf("Can't create loadbalancer %s. Error getting ip addresses for host %s. Error: %#v", lb.Name,
					hostname, err)
			}

			if len(coll.Data) < 1 {
				continue
			}

			exSvc = &client.ExternalService{
				Name:                hostname,
				ExternalIpAddresses: []string{coll.Data[0].Address},
				EnvironmentId:       lb.EnvironmentId,
			}
			exSvc, err = r.client.ExternalService.Create(exSvc)
			if err != nil {
				return fmt.Errorf("Error setting hosts for LB %s. Couldn't create external service for host %s. Error: %#v",
					lb.Name, hostname, err)
			}
		}

		if exSvc.State != "active" {
			actionChannel := r.waitForSvcAction("activate", exSvc)
			svcInterface, ok := <-actionChannel
			if !ok {
				return fmt.Errorf("Couldn't call activate on external service %s for LB %s", exSvc.Id, lb.Name)
			}
			exSvc, ok = svcInterface.(*client.ExternalService)
			if !ok {
				panic(fmt.Sprintf("Couldn't cast to ExternalService type! Interface: %#v", svcInterface))
			}

			_, err = r.client.ExternalService.ActionActivate(exSvc)
			if err != nil {
				return fmt.Errorf("Couldn't activate service for LB %s. Error: %#v", lb.Name, err)
			}
		}
		serviceLinks.ServiceLinks = append(serviceLinks.ServiceLinks, &client.LoadBalancerServiceLink{ServiceId: exSvc.Id})

	}

	actionChannel := r.waitForLBAction("setservicelinks", lb)
	lbInterface, ok := <-actionChannel
	if !ok {
		return fmt.Errorf("Couldn't call setservicelinks on LB %s", lb.Name)
	}
	lb = convertLB(lbInterface)

	_, err := r.client.LoadBalancerService.ActionSetservicelinks(lb, serviceLinks)
	if err != nil {
		return fmt.Errorf("Error setting hosts for LB%s. Couldn't set LB service links. Error: %#v.", lb.Name, err)
	}

	return nil
}

type waitCallback func(result chan<- interface{}) (bool, error)

func (r *CloudProvider) waitForLBAction(action string, lb *client.LoadBalancerService) <-chan interface{} {
	cb := func(result chan<- interface{}) (bool, error) {
		l, err := r.client.LoadBalancerService.ById(lb.Id)
		if err != nil {
			return false, fmt.Errorf("Error waiting for action %s for LB %s. Couldn't get LB by id. Error: %#v.", action, lb.Name, err)
		}
		if _, ok := l.Actions[action]; ok {
			result <- l
			return true, nil
		}
		return false, nil
	}
	return r.waitForAction(action, cb)
}

func (r *CloudProvider) waitForSvcAction(action string, svc *client.ExternalService) <-chan interface{} {
	cb := func(result chan<- interface{}) (bool, error) {
		s, err := r.client.ExternalService.ById(svc.Id)
		if err != nil {
			return false, fmt.Errorf("Error waiting for action %s for service %s. Couldn't get service by id. Error %#v.", action, svc.Name, err)
		}
		if _, ok := s.Actions[action]; ok {
			result <- s
			return true, nil
		}
		return false, nil
	}
	return r.waitForAction(action, cb)
}

func (r *CloudProvider) waitForAction(action string, callback waitCallback) <-chan interface{} {
	ready := make(chan interface{}, 0)
	go func() {
		sleep := 2
		defer close(ready)
		for i := 0; i < 5; i++ {
			foundAction, err := callback(ready)
			if err != nil {
				glog.Errorf("Error: %#v", err)
				return
			}

			if foundAction {
				return
			}
			time.Sleep(time.Second * time.Duration(sleep))
		}
		glog.Errorf("Timed out waiting for action %s.", action)
	}()
	return ready
}

func (r *CloudProvider) getLBByName(name string) (*client.LoadBalancerService, error) {
	opts := client.NewListOpts()
	opts.Filters["name"] = name
	opts.Filters["removed_null"] = "1"
	lbs, err := r.client.LoadBalancerService.List(opts)
	if err != nil {
		return nil, fmt.Errorf("Coudln't get LB by name [%s]. Error: %#v", name, err)
	}

	if len(lbs.Data) == 0 {
		return nil, nil
	}

	if len(lbs.Data) > 1 {
		return nil, fmt.Errorf("Multiple LBs found for name: %s", name)
	}

	return &lbs.Data[0], nil
}

func (r *CloudProvider) toLBStatus(lb *client.LoadBalancerService) (*api.LoadBalancerStatus, bool, error) {
	instAndHosts := &instanceCollection{}
	err := getJSON(lb.Links["instances"], map[string][]string{"include": []string{"hosts"}}, instAndHosts)
	if err != nil {
		return nil, false, err
	}

	ingress := []api.LoadBalancerIngress{}
	for _, i := range instAndHosts.Data {
		for _, h := range i.Hosts {
			hIP := &hostAndIPAddresses{}
			err = getJSON(h.Links["self"], map[string][]string{"include": []string{"ipAddresses"}}, hIP)
			if err != nil {
				return nil, false, err
			}
			for _, ip := range hIP.IPAddresses {
				ingress = append(ingress, api.LoadBalancerIngress{IP: ip.Address})
			}
		}
	}

	return &api.LoadBalancerStatus{ingress}, true, nil
}

func (r *CloudProvider) deleteLoadBalancer(lb *client.LoadBalancerService) error {
	err := r.deleteLBConsumedServices(lb)
	if err != nil {
		return err
	}

	err = r.client.LoadBalancerService.Delete(lb)
	if err != nil {
		return fmt.Errorf("Unable to delete load balancer for service %s. Error: %#v", lb.Name, err)
	}
	return nil
}

func (r *CloudProvider) deleteLBConsumedServices(lb *client.LoadBalancerService) error {
	coll := &client.ServiceCollection{}
	err := r.client.GetLink(lb.Resource, "consumedservices", coll)
	if err != nil {
		return fmt.Errorf("Can't delete consumed services for LB %s. Error getting consumed services. Error: %#v", lb.Name, err)
	}

	for _, service := range coll.Data {
		consumedBy := &client.ServiceCollection{}
		err = r.client.GetLink(lb.Resource, "consumedbyservices", consumedBy)
		if err != nil {
			glog.Errorf("Error getting consumedby services for service %s. This service won't be deleted. Error: %#v",
				lb.Name, service.Id, err)
			continue
		}

		if len(consumedBy.Data) > 1 {
			glog.Infof("Service %s has more than consumer. Will not delete it.", service.Id)
			continue
		}

		err = r.client.Service.Delete(&service)
		if err != nil {
			glog.Warningf("Error deleting service %s. Moving on. Error: %#v", service.Id, err)
		}
	}

	return nil
}

// --- Instances Functions ---

// NodeAddresses returns the addresses of the specified instance.
// This implementation only returns the address of the calling instance. This is ok
// because the gce implementation makes that assumption and the comment for the interface
// states it as a todo to clarify that it is only for the current host
func (r *CloudProvider) NodeAddresses(name string) ([]api.NodeAddress, error) {
	host, err := r.getHostByName(name)
	if err != nil {
		return nil, err
	}

	coll := &client.IpAddressCollection{}
	err = r.client.GetLink(host.Resource, "ipAddresses", coll)
	if err != nil {
		return nil, fmt.Errorf("Error getting ip addresses for node [%s]. Error: %#v", name, err)
	}

	if len(coll.Data) == 0 {
		return nil, cloudprovider.InstanceNotFound
	}

	addresses := []api.NodeAddress{}
	for _, ip := range coll.Data {
		addresses = append(addresses, api.NodeAddress{Type: api.NodeExternalIP, Address: ip.Address})
	}

	return addresses, nil
}

// ExternalID returns the cloud provider ID of the specified instance (deprecated).
func (r *CloudProvider) ExternalID(name string) (string, error) {
	glog.Infof("ExternalID [%s]", name)
	return r.InstanceID(name)
}

// InstanceID returns the cloud provider ID of the specified instance.
func (r *CloudProvider) InstanceID(name string) (string, error) {
	glog.Infof("InstanceID [%s]", name)
	host, err := r.getHostByName(name)
	if err != nil {
		return "", err
	}

	return host.Uuid, nil
}

// List lists instances that match 'filter' which is a regular expression which must match the entire instance name (fqdn)
func (r *CloudProvider) List(filter string) ([]string, error) {
	glog.Infof("List %s", filter)

	opts := client.NewListOpts()
	opts.Filters["removed_null"] = "1"
	hosts, err := r.client.Host.List(opts)
	if err != nil {
		return nil, fmt.Errorf("Coudln't get hosts by filter [%s]. Error: %#v", filter, err)
	}

	if len(hosts.Data) == 0 {
		return nil, fmt.Errorf("No hosts found")
	}

	if strings.HasPrefix(filter, "'") && strings.HasSuffix(filter, "'") {
		filter = filter[1 : len(filter)-1]
	}

	re, err := regexp.Compile(filter)
	if err != nil {
		return nil, err
	}

	retHosts := []string{}
	for _, host := range hosts.Data {
		if re.MatchString(host.Name) {
			retHosts = append(retHosts, host.Name)
		}
	}

	return retHosts, err
}

// AddSSHKeyToAllInstances adds an SSH public key as a legal identity for all instances
// expected format for the key is standard ssh-keygen format: <protocol> <blob>
func (r *CloudProvider) AddSSHKeyToAllInstances(user string, keyData []byte) error {
	return fmt.Errorf("Not implemented")
}

// CurrentNodeName returns the name of the node we are currently running on
func (r *CloudProvider) CurrentNodeName(hostname string) (string, error) {
	return hostname, nil
}

func (r *CloudProvider) getHostByName(name string) (*client.Host, error) {
	opts := client.NewListOpts()
	opts.Filters["name"] = name
	opts.Filters["removed_null"] = "1"
	hosts, err := r.client.Host.List(opts)
	if err != nil {
		return nil, fmt.Errorf("Coudln't get host by name [%s]. Error: %#v", name, err)
	}

	if len(hosts.Data) == 0 {
		return nil, cloudprovider.InstanceNotFound
	}

	if len(hosts.Data) != 1 {
		return nil, fmt.Errorf("multiple instances found for name: %s", name)
	}

	return &hosts.Data[0], nil
}

// --- Zones Functions ---

// GetZone is an implementation of Zones.GetZone
func (r *CloudProvider) GetZone() (cloudprovider.Zone, error) {
	// TODO What makes sense here? Maybe the rancher environment?
	return cloudprovider.Zone{
		FailureDomain: "FailureDomain1",
		Region:        "Region1",
	}, nil
}

// --- Utility functions ---

func init() {
	cloudprovider.RegisterCloudProvider(providerName, func(config io.Reader) (cloudprovider.Interface, error) {
		return newRancherCloud(config)
	})
}

type configGlobal struct {
	CattleURL       string `gcfg:"cattle-url"`
	CattleAccessKey string `gcfg:"cattle-access-key"`
	CattleSecretKey string `gcfg:"cattle-secret-key"`
}

type rConfig struct {
	Global configGlobal
}

func newRancherCloud(config io.Reader) (cloudprovider.Interface, error) {
	url := os.Getenv("CATTLE_URL")
	accessKey := os.Getenv("CATTLE_ACCESS_KEY")
	secretKey := os.Getenv("CATTLE_SECRET_KEY")
	conf := rConfig{
		Global: configGlobal{
			CattleURL:       url,
			CattleAccessKey: accessKey,
			CattleSecretKey: secretKey,
		},
	}
	client, err := getRancherClient(conf)
	if err != nil {
		return nil, fmt.Errorf("Could not create rancher client: %#v", err)
	}

	return &CloudProvider{
		client: client,
	}, nil
}

func getRancherClient(conf rConfig) (*client.RancherClient, error) {
	return client.NewRancherClient(&client.ClientOpts{
		Url:       conf.Global.CattleURL,
		AccessKey: conf.Global.CattleAccessKey,
		SecretKey: conf.Global.CattleSecretKey,
	})
}

func get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	defer resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("Couldn't get %s: %v", url, err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Error ready body of response to [%s]. Error %v", url, err)
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Received unexpected response code for [%s]: [%v]. Response body: [%s]", url, resp.StatusCode, string(body))
	}

	return body, nil
}

func metadata(path string) (string, error) {
	resp, err := http.Get("http://rancher-metadata/latest" + path)
	if err != nil {
		return "", fmt.Errorf("Couldn't get %s: %v", path, err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	ret := string(body)
	if err != nil {
		return "", fmt.Errorf("Couldn't get %s: %v", path, err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Received unexpected response code: [%v], Response body: [%s]", resp.StatusCode, ret)
	}

	return ret, nil
}

func getJSON(url string, params map[string][]string, respObject interface{}) error {
	url, err := addParameters(url, params)
	if err != nil {
		return err
	}

	instanceRaw, err := get(url)
	if err != nil {
		return err
	}

	err = json.Unmarshal(instanceRaw, respObject)
	if err != nil {
		return fmt.Errorf("Couldn't unmarshal response json for [%s]. Error: %#v", url, err)
	}

	return nil
}

func addParameters(baseURL string, params map[string][]string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("Couldn't parse url [%s]. Error: %#v", baseURL, err)
	}
	q := u.Query()
	for key, vals := range params {
		for _, val := range vals {
			q.Add(key, val)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func portsChanged(newPorts []string, oldPorts []string) bool {
	if len(newPorts) != len(oldPorts) {
		return true
	}

	if len(newPorts) == 0 {
		return false
	}

	sort.Strings(newPorts)
	sort.Strings(oldPorts)
	for idx, p := range newPorts {
		if p != oldPorts[idx] {
			return false
		}
	}

	return true
}

func formatLBName(name string) string {
	return fmt.Sprintf(lbNameFormat, name)
}
