package containers

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"

	"git.corout.in/golibs/errors"
)

const reservedNetworksVar = "RESERVED_NETWORKS"

type EndpointSettings struct {
	IPAddress string
}

// NetworksAllocator интерфейс резервирования сетей
type NetworksAllocator struct {
	client Client
	addr   *net.IPNet

	mu   sync.RWMutex
	used map[string]int
}

// NewNetworkAllocator конструктор интерфейса резервирования сетей
func NewNetworkAllocator(client Client) (*NetworksAllocator, error) {
	na := &NetworksAllocator{
		client: client,
		used:   make(map[string]int),
		addr: &net.IPNet{
			IP:   net.IPv4(172, 16, 0, 0),
			Mask: net.IPv4Mask(255, 240, 0, 0),
		},
	}

	if reservedStr := os.Getenv(reservedNetworksVar); reservedStr != "" {
		reserved := strings.Split(reservedStr, ",")

		for i := 0; i < len(reserved); i++ {
			_, reserve, err := net.ParseCIDR(reserved[i])
			if err != nil {
				return nil, errors.Ctx().Str("parsed", reserved[i]).Wrap(err, "parse reserved network")
			}

			sz, _ := reserve.Mask.Size()
			na.used[reserve.String()] = sz
		}
	}

	return na, nil
}

// GetFreeSubnet возвращает адрес свободной сети
func (na *NetworksAllocator) GetFreeSubnet(ctx context.Context) (*net.IPNet, error) {
	na.mu.Lock()
	defer na.mu.Unlock()

	if err := na.getUsedNetworks(ctx); err != nil {
		return nil, errors.Wrap(err, "get used networks")
	}

	max, _ := na.addr.Mask.Size()

	for ni := int(na.addr.IP.To4()[1]); ni < int(na.addr.IP.To4()[1])+max; ni++ {
		global := net.IPNet{
			IP:   net.IPv4(na.addr.IP.To4()[0], byte(ni), 0, 0),
			Mask: net.IPv4Mask(255, 255, 0, 0),
		}

		if sz, ok := na.used[global.String()]; !ok || sz != 16 {
			for si := 0; si < 255; si++ {
				subnet := net.IPNet{
					IP:   net.IPv4(na.addr.IP.To4()[0], global.IP.To4()[1], byte(si), 0),
					Mask: net.IPv4Mask(255, 255, 255, 0),
				}

				if _, exist := na.used[subnet.String()]; !exist {
					na.used[subnet.String()] = 24

					return &subnet, nil
				}
			}
		}
	}

	return nil, nil
}

func (na *NetworksAllocator) getUsedNetworks(ctx context.Context) error {
	list, err := na.client.NetworkList(ctx)
	if err != nil {
		return errors.Wrap(err, "get client networks list")
	}

	for li := 0; li < len(list); li++ {
		sz, _ := list[li].Mask.Size()
		na.used[list[li].String()] = sz
	}

	return nil
}
