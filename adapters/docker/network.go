package docker

import (
	"context"
	"os"
	"strings"
	"sync"

	"git.corout.in/golibs/errors"
	"git.corout.in/golibs/network/ipnet"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	"git.corout.in/golibs/containers"
)

const (
	// DefaultNetworkDriver - драйвер сети докера по умолчанию
	DefaultNetworkDriver = "bridge"
	// ErrDockerNetworkNotExist - ошибка "докер сеть отсутствует"
	ErrDockerNetworkNotExist = errors.Const("docker network dose not exist")

	reservedNetworksVar       = "DOCKER_RESERVED_NETWORKS"
	maxTypeID           uint8 = 40
)

type dockerNetwork struct {
	*types.NetworkResource
	client client.APIClient
	subnet *ipnet.SubnetRange

	mu         sync.RWMutex
	containers [maxTypeID][]*containers.OrchestratorInfo
}

func (nw *dockerNetwork) ID() string {
	if nw != nil {
		return nw.NetworkResource.ID
	}

	return ""
}

func (nw *dockerNetwork) Name() string {
	if nw != nil {
		return nw.NetworkResource.Name
	}

	return ""
}

func (nw *dockerNetwork) Gateway() string {
	if nw != nil && len(nw.IPAM.Config) != 0 {
		return nw.IPAM.Config[0].Gateway
	}

	return ""
}

func (nw *dockerNetwork) HostIP() string {
	ip := nw.subnet.NextIP()

	for ip != "" && !nw.isFreeIP(ip) {
		ip = nw.subnet.NextIP()
	}

	return ip
}

func (nw *dockerNetwork) NextIP() string {
	ip := nw.subnet.NextIP()

	for ip != "" && !nw.isFreeIP(ip) {
		ip = nw.subnet.NextIP()
	}

	return ip
}

func (nw *dockerNetwork) AddContainer(info *containers.OrchestratorInfo) {
	if info.TypeID > uint8(len(nw.containers)) {
		panic("containers types overflow")
	}

	nw.mu.Lock()
	defer nw.mu.Unlock()

	nw.containers[info.TypeID] = append(nw.containers[info.TypeID], info)
}

func (nw *dockerNetwork) isFreeIP(ip string) bool {
	resource, err := nw.client.NetworkInspect(context.Background(), nw.ID(), types.NetworkInspectOptions{})
	if err != nil {
		return false
	}

	for _, endpoint := range resource.Containers {
		if strings.Contains(endpoint.IPv4Address, ip) {
			return false
		}
	}

	return true
}

func getReservedNetworks() []string {
	if reservedStr := os.Getenv(reservedNetworksVar); reservedStr != "" {
		return strings.Split(reservedStr, ",")
	}

	return nil
}
