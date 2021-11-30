package main

import (
	"bytes"
	compute "cloud.google.com/go/compute/apiv1"
	container "cloud.google.com/go/container/apiv1"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	computepb "google.golang.org/genproto/googleapis/cloud/compute/v1"
	containerpb "google.golang.org/genproto/googleapis/container/v1"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type GoogleCloudClient interface {
	addAccessConfig(string, string) error
	deleteAccessConfigByName(string, string) error
	getAccessConfigsByInstanceName(string) ([]*AccessConfig, error)
	getAddressByName(string) (*string, error)
	getInstanceGroupByNodePoolName(string, string) (*string, error)
	getInstancesByInstanceGroupName(string) ([]*string, error)
	setNodePoolIP(string, string, string) (*string, error)
}

type AccessConfig struct {
	name  string
	natIP string
}

type GoogleCloudSdkClient struct {
	project *string
	region  *string
	zone    *string

	addresses      *compute.AddressesClient
	clusterManager *container.ClusterManagerClient
	zoneOperations *compute.ZoneOperationsClient
	instanceGroups *compute.InstanceGroupsClient
	instances      *compute.InstancesClient

	cloudRunClient *http.Client
	cloudRunUrl    *string
}

func (client GoogleCloudSdkClient) getAddressByName(name string) (*string, error) {
	req := &computepb.GetAddressRequest{Address: name, Project: *client.project, Region: *client.region}
	address, err := client.addresses.Get(context.Background(), req)
	if address == nil {
		return nil, err
	}
	return address.Address, err
}

func (client GoogleCloudSdkClient) getInstanceGroupByNodePoolName(clusterName string, nodePoolName string) (*string, error) {
	req := &containerpb.GetNodePoolRequest{
		Name: fmt.Sprintf("projects/%v/zones/%v/clusters/%v/nodePools/%v", *client.project, *client.zone, clusterName, nodePoolName),
	}
	nodePool, err := client.clusterManager.GetNodePool(context.Background(), req)
	if nodePool == nil {
		return nil, err
	}
	return basename(nodePool.GetInstanceGroupUrls()[0]), err
}

func (client GoogleCloudSdkClient) getInstancesByInstanceGroupName(instanceGroupName string) ([]*string, error) {
	req := &computepb.ListInstancesInstanceGroupsRequest{InstanceGroup: instanceGroupName, Project: *client.project, Zone: *client.zone}
	instances := client.instanceGroups.ListInstances(context.Background(), req)
	instanceNames := make([]*string, 0)
	for {
		instance, err := instances.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		instanceNames = append(instanceNames, basename(instance.GetInstance()))
	}
	return instanceNames, nil
}

func (client GoogleCloudSdkClient) getAccessConfigsByInstanceName(name string) ([]*AccessConfig, error) {
	req := &computepb.GetInstanceRequest{Instance: name, Project: *client.project, Zone: *client.zone}
	instance, err := client.instances.Get(context.Background(), req)
	if instance == nil {
		return nil, err
	}
	networkInterface := instance.GetNetworkInterfaces()[0]
	accessConfigs := make([]*AccessConfig, 0)
	for _, accessConfig := range networkInterface.GetAccessConfigs() {
		accessConfigs = append(accessConfigs, &AccessConfig{
			name:  accessConfig.GetName(),
			natIP: accessConfig.GetNatIP(),
		})
	}
	return accessConfigs, err
}

func (client GoogleCloudSdkClient) deleteAccessConfigByName(instanceName string, name string) error {
	req := &computepb.DeleteAccessConfigInstanceRequest{
		Instance:         instanceName,
		NetworkInterface: "nic0",
		AccessConfig:     name,
		Project:          *client.project,
		Zone:             *client.zone,
	}
	op, err := client.instances.DeleteAccessConfig(context.Background(), req)
	if err != nil {
		return err
	}
	status := op.Proto().GetStatus()
	for status != computepb.Operation_DONE {
		log.Printf("Current status is %v, sleeping for 2 seconds...\n", status)
		time.Sleep(2 * time.Second)
		opReq := &computepb.GetZoneOperationRequest{Operation: op.Proto().GetName(), Project: *client.project, Zone: *client.zone}
		opResp, err := client.zoneOperations.Get(context.Background(), opReq)
		if err != nil {
			return err
		}
		status = opResp.GetStatus()
	}
	log.Print("Deleting access config successful...")
	return nil
}

func (client GoogleCloudSdkClient) addAccessConfig(instanceName string, address string) error {
	req := &computepb.AddAccessConfigInstanceRequest{
		Instance:         instanceName,
		NetworkInterface: "nic0",
		AccessConfigResource: &computepb.AccessConfig{
			NatIP: &address,
		},
		Project: *client.project,
		Zone:    *client.zone,
	}
	op, err := client.instances.AddAccessConfig(context.Background(), req)
	if err != nil {
		return err
	}
	status := op.Proto().GetStatus()
	for status != computepb.Operation_DONE {
		log.Printf("Current status is %v, sleeping for 2 seconds...\n", status)
		time.Sleep(2 * time.Second)
		opReq := &computepb.GetZoneOperationRequest{Operation: op.Proto().GetName(), Project: *client.project, Zone: *client.zone}
		opResp, err := client.zoneOperations.Get(context.Background(), opReq)
		if err != nil {
			return err
		}
		status = opResp.GetStatus()
	}
	log.Print("Adding access config successful...")
	return nil
}

type NodePoolIpRequest struct {
	Cluster    string `json:"cluster"`
	NodePool   string `json:"node_pool"`
	ExternalIP string `json:"external_ip"`
}

func (client GoogleCloudSdkClient) setNodePoolIP(cluster string, nodePool string, externalIP string) (*string, error) {
	req := NodePoolIpRequest{
		Cluster:    cluster,
		NodePool:   nodePool,
		ExternalIP: externalIP,
	}
	reqJson, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := client.cloudRunClient.Post(*client.cloudRunUrl+"/run", "application/json", bytes.NewBuffer(reqJson))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	respBodyString := string(respBody)
	return &respBodyString, nil
}

func (client GoogleCloudSdkClient) close() {
	_ = client.addresses.Close()
	_ = client.clusterManager.Close()
	_ = client.instanceGroups.Close()
	_ = client.instances.Close()
	_ = client.zoneOperations.Close()
}

func getenv(key string) *string {
	value, set := os.LookupEnv(key)
	if !set || len(value) == 0 {
		log.Printf("environment variable '%v' is not set\n", key)
		return nil
	}
	return &value
}

func basename(url string) *string {
	components := strings.Split(url, "/")
	part := components[len(components)-1]
	return &part
}

func filter(accessConfigs []*AccessConfig, test func(accessConfig *AccessConfig) bool) []*AccessConfig {
	filtered := make([]*AccessConfig, 0)
	for _, e := range accessConfigs {
		if test(e) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func main() {
	log.Print("starting server...")

	project := getenv("GOOGLE_CLOUD_PROJECT")
	region := getenv("GOOGLE_CLOUD_REGION")
	zone := getenv("GOOGLE_CLOUD_ZONE")

	if project == nil || region == nil || zone == nil {
		log.Print("exiting...")
		return
	}

	// Determine port for HTTP service.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("defaulting to port %s", port)
	}

	ctx := context.Background()

	addresses, err := compute.NewAddressesRESTClient(ctx)
	if err != nil {
		log.Print(err)
		return
	}

	clusterManager, err := container.NewClusterManagerClient(ctx)
	if err != nil {
		log.Print(err)
		return
	}

	zoneOperations, err := compute.NewZoneOperationsRESTClient(ctx)
	if err != nil {
		log.Print(err)
		return
	}

	instanceGroups, err := compute.NewInstanceGroupsRESTClient(ctx)
	if err != nil {
		log.Print(err)
		return
	}

	instances, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		log.Print(err)
		return
	}

	client := GoogleCloudSdkClient{
		project: project,
		region:  region,
		zone:    zone,

		addresses:      addresses,
		clusterManager: clusterManager,
		zoneOperations: zoneOperations,
		instanceGroups: instanceGroups,
		instances:      instances,
	}

	defer client.close()

	http.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		handleRun(w, r, client)
	})

	// Start HTTP server.
	log.Printf("listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleRun(w http.ResponseWriter, r *http.Request, client GoogleCloudSdkClient) {
	traceId := uuid.New()
	if r.Method == http.MethodPost {
		if r.Header.Get("Content-Type") == "application/json" {
			var body NodePoolIpRequest
			err := json.NewDecoder(r.Body).Decode(&body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Printf("[%v] recieved http call: method=%v, body=%v", traceId, r.Method, body)
			go func() {
				err := run(client, body.Cluster, body.NodePool, body.ExternalIP)
				if err != nil {
					log.Printf("[%v] error during process: %v", traceId, err)
				} else {
					log.Printf("[%v] process successful", traceId)
				}
			}()
		} else {
			w.WriteHeader(http.StatusUnsupportedMediaType)
		}
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func run(client GoogleCloudClient, cluster string, nodePool string, address string) error {
	log.Printf("Running IP check: cluster=%v, nodePool=%v, address=%v\n", cluster, nodePool, address)

	ip, err := client.getAddressByName(address)
	if err != nil {
		return err
	}
	log.Printf("Found IP: %v\n", *ip)

	instanceGroup, err := client.getInstanceGroupByNodePoolName(cluster, nodePool)
	if err != nil {
		return err
	}
	log.Printf("Found instance group: %v\n", *instanceGroup)

	instances, err := client.getInstancesByInstanceGroupName(*instanceGroup)
	if err != nil {
		return err
	}

	networkMap := make(map[string][]*AccessConfig)

	for _, instance := range instances {
		log.Printf("Found instance: %v\n", *instance)
		accessConfig, err := client.getAccessConfigsByInstanceName(*instance)
		if err != nil {
			return err
		}

		networkMap[*instance] = accessConfig
	}

	isIpAssigned := false

	for _, accessConfigs := range networkMap {
		for _, accessConfig := range accessConfigs {
			if accessConfig.natIP == *ip {
				isIpAssigned = true
				break
			}
		}
		if isIpAssigned {
			break
		}
	}

	if !isIpAssigned {
		instance := instances[0]
		accessConfigs := networkMap[*instance]
		externalNats := filter(accessConfigs, func(accessConfig *AccessConfig) bool {
			return accessConfig.name == "external-nat" || accessConfig.name == "External NAT"
		})
		for _, accessConfig := range externalNats {
			log.Printf("Removing existing access configs from %v\n", *instance)
			err := client.deleteAccessConfigByName(*instance, accessConfig.name)
			if err != nil {
				return err
			}
		}
		log.Printf("Assigning IP to %v\n", *instance)
		err = client.addAccessConfig(*instance, *ip)
		if err != nil {
			return err
		}
		return nil
	} else {
		log.Print("IP is already assigned.")
	}

	return nil
}
