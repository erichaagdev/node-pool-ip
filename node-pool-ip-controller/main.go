package main

import (
	compute "cloud.google.com/go/compute/apiv1"
	container "cloud.google.com/go/container/apiv1"
	"context"
	"fmt"
	"google.golang.org/api/iterator"
	computepb "google.golang.org/genproto/googleapis/cloud/compute/v1"
	containerpb "google.golang.org/genproto/googleapis/container/v1"
	"os"
	"strings"
	"time"
)

func getenv(key string) *string {
	value, set := os.LookupEnv(key)
	if !set || len(value) == 0 {
		fmt.Printf("environment variable '%v' is not set\n", key)
		return nil
	}
	return &value
}

func basename(url string) *string {
	components := strings.Split(url, "/")
	part := components[len(components)-1]
	return &part
}

type GoogleCloudClient interface {
	addAccessConfig(string, string, string) error
	deleteAccessConfigByName(string, string, string) error
	getAccessConfigsByInstanceName(string) ([]*AccessConfig, error)
	getAddressByName(string) (*string, error)
	getInstanceGroupByNodePoolName(string, string) (*string, error)
	getInstancesByInstanceGroupName(string) ([]*string, error)
}

type AccessConfig struct {
	network string
	name    string
	natIP   string
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
			network: networkInterface.GetName(),
			name:    accessConfig.GetName(),
			natIP:   accessConfig.GetNatIP(),
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
		fmt.Printf("Current status is %v, sleeping for 2 seconds...\n", status)
		time.Sleep(2 * time.Second)
		opReq := &computepb.GetZoneOperationRequest{Operation: op.Proto().GetName(), Project: *client.project, Zone: *client.zone}
		opResp, err := client.zoneOperations.Get(context.Background(), opReq)
		if err != nil {
			return err
		}
		status = opResp.GetStatus()
	}
	fmt.Println("Deleting access config successful...")
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
		fmt.Printf("Current status is %v, sleeping for 2 seconds...\n", status)
		time.Sleep(2 * time.Second)
		opReq := &computepb.GetZoneOperationRequest{Operation: op.Proto().GetName(), Project: *client.project, Zone: *client.zone}
		opResp, err := client.zoneOperations.Get(context.Background(), opReq)
		if err != nil {
			return err
		}
		status = opResp.GetStatus()
	}
	fmt.Println("Adding access config successful...")
	return nil
}

func (client GoogleCloudSdkClient) close() {
	_ = client.addresses.Close()
	_ = client.clusterManager.Close()
	_ = client.instanceGroups.Close()
	_ = client.instances.Close()
	_ = client.zoneOperations.Close()
}

func main() {

	fmt.Printf("hello from node-pool-ip-controller!")

	//project := getenv("GOOGLE_CLOUD_PROJECT")
	//region := getenv("GOOGLE_CLOUD_REGION")
	//zone := getenv("GOOGLE_CLOUD_ZONE")
	//cluster := getenv("GOOGLE_CLOUD_CONTAINER_CLUSTER")
	//nodePool := getenv("GOOGLE_CLOUD_CONTAINER_NODE_POOL")
	//address := getenv("GOOGLE_CLOUD_COMPUTE_ADDRESS")
	//
	//if project == nil || region == nil || zone == nil || cluster == nil || nodePool == nil || address == nil {
	//	fmt.Println("exiting...")
	//	return
	//}
	//
	//ctx := context.Background()
	//
	//addresses, err := compute.NewAddressesRESTClient(ctx)
	//if err != nil {
	//	fmt.Println(err)
	//	return
	//}
	//
	//clusterManager, err := container.NewClusterManagerClient(ctx)
	//if err != nil {
	//	fmt.Println(err)
	//	return
	//}
	//
	//zoneOperations, err := compute.NewZoneOperationsRESTClient(ctx)
	//if err != nil {
	//	fmt.Println(err)
	//	return
	//}
	//
	//instanceGroups, err := compute.NewInstanceGroupsRESTClient(ctx)
	//if err != nil {
	//	fmt.Println(err)
	//	return
	//}
	//
	//instances, err := compute.NewInstancesRESTClient(ctx)
	//if err != nil {
	//	fmt.Println(err)
	//	return
	//}
	//
	//client := GoogleCloudSdkClient{
	//	project: project,
	//	region:  region,
	//	zone:    zone,
	//
	//	addresses:      addresses,
	//	clusterManager: clusterManager,
	//	zoneOperations: zoneOperations,
	//	instanceGroups: instanceGroups,
	//	instances:      instances,
	//}
	//
	//defer client.close()
	//
	//err = run(client, *cluster, *nodePool, *address)
	//if err != nil {
	//	fmt.Println("**ERROR**", err)
	//	os.Exit(1)
	//}
}

func run(client GoogleCloudClient, cluster string, nodePool string, address string) error {
	fmt.Printf("Running IP check: cluster=%v, nodePool=%v, address=%v\n", cluster, nodePool, address)

	ip, err := client.getAddressByName(address)
	if err != nil {
		return err
	}
	fmt.Printf("Found IP: %v\n", *ip)

	instanceGroup, err := client.getInstanceGroupByNodePoolName(cluster, nodePool)
	if err != nil {
		return err
	}
	fmt.Printf("Found instance group: %v\n", *instanceGroup)

	instances, err := client.getInstancesByInstanceGroupName(*instanceGroup)
	if err != nil {
		return err
	}

	networkMap := make(map[string][]*AccessConfig)

	for _, instance := range instances {
		fmt.Printf("Found instance: %v\n", *instance)
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
			fmt.Printf("Removing existing access configs from %v\n", *instance)
			err := client.deleteAccessConfigByName(*instance, accessConfig.network, accessConfig.name)
			if err != nil {
				return err
			}
		}
		fmt.Printf("Assigning IP to %v\n", *instance)
		err = client.addAccessConfig(*instance, "nic0", *ip)
		if err != nil {
			return err
		}
		return nil
	} else {
		fmt.Println("IP is already assigned.")
	}

	return nil
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
