package rancher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/google/cadvisor/events"
	info "github.com/google/cadvisor/info/v1"
	infov2 "github.com/google/cadvisor/info/v2"
)

// Client represents the base URL for a cAdvisor client.
type Client struct {
	baseUrl string
}

// NewClient returns a new client with the specified base URL.
func NewClient(url string) (*Client, error) {
	if !strings.HasSuffix(url, "/") {
		url += "/api/"
	}

	return &Client{
		baseUrl: url,
	}, nil
}

// Returns the JSON container information for the specified
// Docker container and request.
func (self *Client) DockerContainer(name string, query *info.ContainerInfoRequest) (cinfo info.ContainerInfo, err error) {
	u := self.dockerInfoUrl(name)
	ret := make(map[string]info.ContainerInfo)
	if err = self.httpGetJsonData(&ret, query, u, fmt.Sprintf("Docker container info for %q", name)); err != nil {
		return
	}
	if len(ret) != 1 {
		err = fmt.Errorf("expected to only receive 1 Docker container: %+v", ret)
		return
	}
	for _, cont := range ret {
		cinfo = cont
	}
	return
}

// ContainerInfo returns the JSON container information for the specified
// container and request.
func (self *Client) ContainerInfo(name string, query *info.ContainerInfoRequest) (cinfo *info.ContainerInfo, err error) {
	u := self.containerInfoUrl(name)
	ret := new(info.ContainerInfo)
	if err = self.httpGetJsonData(ret, query, u, fmt.Sprintf("container info for %q", name)); err != nil {
		return
	}
	cinfo = ret
	return
}

func (self *Client) ContainerInfoV2(name string, options infov2.RequestOptions) (cinfo map[string]infov2.ContainerInfo, err error) {
	u := self.containerInfoUrl(name)
	ret := map[string]infov2.ContainerInfo{}
	if err = self.httpGetJsonData(&ret, &options, u, fmt.Sprintf("container info for %q", name)); err != nil {
		return
	}
	cinfo = ret
	return
}

// Returns the information about all subcontainers (recursive) of the specified container (including itself).
func (self *Client) SubcontainersInfo(name string, query *info.ContainerInfoRequest) ([]*info.ContainerInfo, error) {
	var response []*info.ContainerInfo
	url := self.subcontainersInfoUrl(name)
	err := self.httpGetJsonData(&response, query, url, fmt.Sprintf("subcontainers container info for %q", name))
	if err != nil {
		return nil, err

	}
	return response, nil
}

// MachineInfo returns the JSON machine information for this client.
// A non-nil error result indicates a problem with obtaining
// the JSON machine information data.
func (self *Client) MachineInfo() (minfo *info.MachineInfo, err error) {
	u := self.machineInfoUrl()
	ret := new(info.MachineInfo)
	if err = self.httpGetJsonData(ret, nil, u, "machine info"); err != nil {
		return
	}
	minfo = ret
	return
}

// Attributes returns hardware and software attributes of the machine.
func (self *Client) Attributes() (attr *infov2.Attributes, err error) {
	u := self.attributesUrl()
	ret := new(infov2.Attributes)
	if err = self.httpGetJsonData(ret, nil, u, "attributes"); err != nil {
		return
	}
	attr = ret
	return
}

// Returns usage information about the filesystem .
func (self *Client) StorageInfo(label string) (sinfo []infov2.FsInfo, err error) {
	u := self.storageInfoUrl(label)

	if err = self.httpGetJsonData(&sinfo, nil, u, "storage info"); err != nil {
		return
	}
	return
}

// Streams all events that occur that satisfy the request into the channel
// that is passed
func (self *Client) EventStreamingInfo(request *events.Request, einfo chan *info.Event) (err error) {
	u := self.eventsInfoUrl(request)
	if err = self.getEventStreamingData(u, einfo); err != nil {
		return
	}
	return nil
}

func (self *Client) dockerInfoUrl(name string) string {
	return self.versionedUrl("v1.3", "docker", name)
}

func (self *Client) containerInfoUrl(name string) string {
	return self.versionedUrl("v1.3", "containers", name)
}

func (self *Client) subcontainersInfoUrl(name string) string {
	return self.versionedUrl("v1.3", "subcontainers", name)
}

func (self *Client) machineInfoUrl() string {
	return self.versionedUrl("v1.3", "machine")
}

func (self *Client) attributesUrl() string {
	return self.versionedUrl("v2.0", "attributes")
}

func (self *Client) storageInfoUrl(label string) string {
	if label != "" {
		q := url.Values{}
		q.Add("label", label)
		label = "?" + q.Encode()
	}
	return self.versionedUrl("v2.0", "storage") + label
}

func (self *Client) eventsInfoUrl(request *events.Request) string {
	eventTypes := map[info.EventType]string{
		info.EventOom:               "oom_events",
		info.EventOomKill:           "oom_kill_events",
		info.EventContainerCreation: "creation_events",
		info.EventContainerDeletion: "deletion_events",
	}

	q := url.Values{}
	q.Add("stream", "true")

	for et, val := range request.EventType {
		if ev, ok := eventTypes[et]; ok {
			q.Add(ev, strconv.FormatBool(val))
		}
	}

	if request.IncludeSubcontainers {
		q.Add("subcontainers", "true")
	}

	if request.MaxEventsReturned > 0 {
		q.Add("max_events", strconv.Itoa(request.MaxEventsReturned))
	}

	if !request.StartTime.IsZero() {
		q.Add("start_time", request.StartTime.Format(time.RFC3339))
	}
	if !request.EndTime.IsZero() {
		q.Add("end_time", request.EndTime.Format(time.RFC3339))
	}

	query := q.Encode()
	if query != "" {
		query = "?" + query
	}

	name := request.ContainerName
	return self.versionedUrl("v1.3", "events", name) + query
}

func (self *Client) versionedUrl(version string, components ...string) string {
	return self.baseUrl + version + "/" + path.Join(components...)
}

func (self *Client) httpGetResponse(postData interface{}, url, infoName string) ([]byte, error) {
	var resp *http.Response
	var err error

	if postData != nil {
		data, marshalErr := json.Marshal(postData)
		if marshalErr != nil {
			return nil, fmt.Errorf("Unable to marshal data: %v", marshalErr)
		}
		resp, err = http.Post(url, "application/json", bytes.NewBuffer(data))
	} else {
		resp, err = http.Get(url)
	}
	if err != nil {
		return nil, fmt.Errorf("Unable to post %q to %q: %v", infoName, url, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("Received empty response for %q from %q", infoName, url)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("Unable to read all %q from %q: %v", infoName, url, err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Request %q failed with error: %q", url, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (self *Client) httpGetString(url, infoName string) (string, error) {
	body, err := self.httpGetResponse(nil, url, infoName)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (self *Client) httpGetJsonData(data, postData interface{}, url, infoName string) error {
	body, err := self.httpGetResponse(postData, url, infoName)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, data); err != nil {
		err = fmt.Errorf("Unable to unmarshal %q (Body: %q) from %q with error: %v", infoName, string(body), url, err)
		return err
	}
	return nil
}

func (self *Client) getEventStreamingData(url string, einfo chan *info.Event) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
	}

	dec := json.NewDecoder(resp.Body)
	var m *info.Event = &info.Event{}
	for {
		err := dec.Decode(m)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			// if called without &stream=true will not be able to parse event and will trigger fatal
			glog.Fatalf("Received error %v", err)
		}
		einfo <- m
	}
	return nil
}
