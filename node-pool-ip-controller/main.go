package main

import (
	"bytes"
	compute "cloud.google.com/go/compute/apiv1"
	container "cloud.google.com/go/container/apiv1"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/api/idtoken"
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
	addAccessConfig(string, string, string) error
	deleteAccessConfigByName(string, string, string) error
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

func (client GoogleCloudSdkClient) deleteAccessConfigByName(instanceName string, networkName string, name string) error {
	req := &computepb.DeleteAccessConfigInstanceRequest{
		Instance:         instanceName,
		NetworkInterface: networkName,
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

func (client GoogleCloudSdkClient) addAccessConfig(instanceName string, networkName string, address string) error {
	req := &computepb.AddAccessConfigInstanceRequest{
		Instance:         instanceName,
		NetworkInterface: networkName,
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
	Project    string `json:"project"`
	Region     string `json:"region"`
	Zone       string `json:"zone"`
	Cluster    string `json:"cluster"`
	NodePool   string `json:"node_pool"`
	ExternalIP string `json:"external_ip"`
}

func (client GoogleCloudSdkClient) setNodePoolIP(cluster string, nodePool string, externalIP string) (*string, error) {
	req := NodePoolIpRequest{
		Project:    *client.project,
		Region:     *client.region,
		Zone:       *client.zone,
		Cluster:    cluster,
		NodePool:   nodePool,
		ExternalIP: externalIP,
	}
	reqJson, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := client.cloudRunClient.Post(*client.cloudRunUrl, "application/json", bytes.NewBuffer(reqJson))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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

func main() {
	cloudRunUrl := getenv("CLOUD_RUN_URL")
	project := getenv("GOOGLE_CLOUD_PROJECT")
	region := getenv("GOOGLE_CLOUD_REGION")
	zone := getenv("GOOGLE_CLOUD_ZONE")
	cluster := getenv("GOOGLE_CLOUD_CONTAINER_CLUSTER")
	nodePool := getenv("GOOGLE_CLOUD_CONTAINER_NODE_POOL")
	address := getenv("GOOGLE_CLOUD_COMPUTE_ADDRESS")

	if cloudRunUrl == nil || project == nil || region == nil || zone == nil || cluster == nil || nodePool == nil || address == nil {
		log.Print("exiting...")
		return
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

	cloudRunClient, err := idtoken.NewClient(ctx, *cloudRunUrl)
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

		cloudRunUrl:    cloudRunUrl,
		cloudRunClient: cloudRunClient,
	}

	defer client.close()

	for {
		err = run(client, *cluster, *nodePool, *address)
		if err != nil {
			log.Print("**ERROR**", err)
		}
		time.Sleep(60 * time.Second)
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
		log.Print("calling node-pool-ip-func")
		resp, err := client.setNodePoolIP(cluster, nodePool, address)
		if err != nil {
			return err
		}
		log.Print(*resp)
	} else {
		log.Print("IP is already assigned.")
	}

	return nil
}
