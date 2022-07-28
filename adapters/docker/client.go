package docker

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"git.corout.in/golibs/errors"
	"git.corout.in/golibs/network/ipnet"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"

	"git.corout.in/golibs/containers"
)

type dockerClient struct {
	client        client.APIClient
	info          types.Info
	netalloc      *ipnet.NetworksAllocator
	stdout        io.Writer
	stderr        io.Writer
	isInContainer bool
}

func New() (containers.Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "create docker client")
	}

	dockerCli := &dockerClient{
		client:        cli,
		stdout:        os.Stdout,
		stderr:        os.Stderr,
		isInContainer: inContainer(),
	}

	dockerCli.info, err = cli.Info(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "get docker info")
	}

	dockerCli.netalloc, err = ipnet.NewNetworkAllocator(
		dockerCli.getUsedNetworks,
		getReservedNetworks()...,
	)
	if err != nil {
		return nil, errors.Wrap(err, "create networks allocator")
	}

	return dockerCli, nil
}

func (cli *dockerClient) WithStdout(w io.Writer) containers.Client {
	cli.stdout = w

	return cli
}

func (cli *dockerClient) WithStderr(w io.Writer) containers.Client {
	cli.stderr = w

	return cli
}

func (cli *dockerClient) IsInContainer() bool {
	return cli.isInContainer
}

func (cli *dockerClient) NetworkList(ctx context.Context) ([]*net.IPNet, error) {
	list, err := cli.client.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "get docker networks list")
	}

	nwList := make([]*net.IPNet, 0, len(list))

	for li := 0; li < len(list); li++ {
		config := list[li].IPAM.Config
		if len(config) == 0 {
			continue
		}

		var nw *net.IPNet

		_, nw, err = net.ParseCIDR(config[0].Subnet)
		if err != nil {
			return nil, errors.Wrap(err, "parse docker network cidr")
		}

		nwList = append(nwList, nw)
	}

	return nwList, nil
}

func (cli *dockerClient) NextSubnet() (*net.IPNet, error) {
	subnet, err := cli.netalloc.GetFreeSubnet(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "get next free subnet")
	}

	return subnet, nil
}

func (cli *dockerClient) RemoveNetwork(id string) error {
	return cli.client.NetworkRemove(context.Background(), id)
}

func (cli *dockerClient) ContainerCreate(ctx context.Context, data containers.Container) (string, error) {
	conf := makeContainerConfig(data)

	cont, err := cli.client.ContainerCreate(
		ctx,
		conf.Config,
		conf.HostConfig,
		conf.NetworkingConfig,
		nil,
		data.GetName(),
	)
	if err != nil {
		return "", errors.Wrap(err, "crete docker container")
	}

	return cont.ID, nil
}

func (cli *dockerClient) ContainerStart(ctx context.Context, id, name string) (*containers.ContainerInfo, error) {
	if err := cli.client.ContainerStart(ctx, id, types.ContainerStartOptions{}); err != nil {
		return nil, errors.Wrapf(err, "start container %s (%s)", name, id[:12])
	}

	list, err := cli.client.ContainerList(
		ctx, types.ContainerListOptions{
			All:     true,
			Filters: filters.NewArgs(filters.Arg("name", name)),
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "search container")
	}

	if len(list) >= 1 {
		var cont types.ContainerJSON

		cont, err = cli.client.ContainerInspect(ctx, list[0].ID)
		if err != nil {
			return nil, errors.Wrap(err, "get container info")
		}

		info := &containers.ContainerInfo{
			ID:        cont.ID,
			IPAddress: cont.NetworkSettings.IPAddress,
			PortBinds: make(map[containers.Port][]containers.PortBinding),
			Networks:  make(map[string]containers.EndpointSettings),
		}

		for port, binds := range cont.HostConfig.PortBindings {
			for pbi := 0; pbi < len(binds); pbi++ {
				info.PortBinds[containers.Port(port)] = append(info.PortBinds[containers.Port(port)],
					containers.PortBinding(binds[pbi]),
				)
			}
		}

		for k, v := range cont.NetworkSettings.Networks {
			info.Networks[k] = containers.EndpointSettings{
				IPAddress: v.IPAddress,
			}
		}

		return info, nil
	}

	return nil, nil
}

func (cli *dockerClient) ContainerWait(ctx context.Context, id string) (<-chan containers.ContainerStatus, <-chan error) {
	waitCh, errCh := cli.client.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	statusCh := make(chan containers.ContainerStatus)

	go func() {
		for status := range waitCh {
			statusMsg := containers.ContainerStatus{
				StatusCode: status.StatusCode,
			}

			if status.Error != nil {
				statusMsg.Error = errors.Ctx().
					Str("message", status.Error.Message).
					Int64("error-code", status.StatusCode).
					New("container status error")
			}

			statusCh <- statusMsg
		}
	}()

	return statusCh, errCh
}

func (cli *dockerClient) ContainerStop(ctx context.Context, id string, timeout time.Duration) error {
	if err := cli.client.ContainerStop(ctx, id, &timeout); err != nil {
		return errors.Wrap(err, "docker container stop")
	}

	return nil
}

func (cli *dockerClient) StreamLogs(ctx context.Context, id string, stderr, stdout io.Writer, follow bool) error {
	if stderr == nil && stdout == nil {
		return nil
	}

	logOptions := types.ContainerLogsOptions{
		ShowStderr: stderr != nil,
		ShowStdout: stdout != nil,
		Follow:     follow,
	}

	logs, err := cli.client.ContainerLogs(ctx, id, logOptions)
	if err != nil {
		return errors.Wrap(err, "container logs streaming")
	}

	var n int64

	for {
		n, err = stdcopy.StdCopy(stdout, stderr, logs)
		if err != nil {
			return errors.Wrap(err, "read containers logs")
		}

		if n == 0 {
			return nil
		}
	}
}

func (cli *dockerClient) CheckDockerNetwork(nw, cidr string) (dn containers.Network, err error) {
	dn, err = cli.checkNetworkExist(nw)
	if err != nil {
		if errors.Is(err, ErrDockerNetworkNotExist) {
			var subnet *ipnet.SubnetRange

			if cidr != "" {
				subnet, err = createSubnetRange(cidr)
				if err != nil {
					return nil, errors.Wrap(err, "get subnet from cidr")
				}
			}

			dn, err = cli.createNetwork(nw, subnet)
			if err != nil {
				return nil, errors.Wrap(err, "create network")
			}

			return dn, nil
		}

		return nil, errors.Wrap(err, "check network")
	}

	return dn, err
}

func (cli *dockerClient) checkNetworkExist(name string) (*dockerNetwork, error) {
	list, err := cli.client.NetworkList(
		context.Background(), types.NetworkListOptions{
			Filters: filters.NewArgs(filters.Arg("name", name)),
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "get network list")
	}

	var subnet *ipnet.SubnetRange

	for i := 0; i < len(list); i++ {
		n := list[i]
		if n.Name == name {
			if ipamCfg := n.IPAM.Config; len(ipamCfg) != 0 {
				subnet, err = createSubnetRange(ipamCfg[0].Subnet)
				if err != nil {
					return nil, errors.Wrap(err, "get subnet from cidr")
				}
			}

			return &dockerNetwork{
				NetworkResource: &n,
				client:          cli.client,
				subnet:          subnet,
			}, nil
		}
	}

	return nil, ErrDockerNetworkNotExist
}

func (cli *dockerClient) createNetwork(name string, subnet *ipnet.SubnetRange) (*dockerNetwork, error) {
	opts := types.NetworkCreate{
		Driver: DefaultNetworkDriver,
	}

	if subnet != nil {
		opts.IPAM = &network.IPAM{
			Config: []network.IPAMConfig{
				{Subnet: subnet.Subnet()},
			},
		}
	}

	resp, err := cli.client.NetworkCreate(context.Background(), name, opts)
	if err != nil {
		return nil, errors.Wrap(err, "docker: create network")
	}

	resource, err := cli.client.NetworkInspect(context.Background(), resp.ID, types.NetworkInspectOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "inspect created network")
	}

	if subnet == nil {
		subnet, err = createSubnetRange(resource.IPAM.Config[0].Subnet)
		if err != nil {
			return nil, errors.Wrap(err, "create network subnet")
		}
	}

	return &dockerNetwork{NetworkResource: &resource, client: cli.client, subnet: subnet}, nil
}

func (cli *dockerClient) getUsedNetworks(ctx context.Context) (ipnet.NetworksSet, error) {
	set := make(ipnet.NetworksSet)

	list, err := cli.client.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "get docker networks list")
	}

	for li := 0; li < len(list); li++ {
		config := list[li].IPAM.Config
		if len(config) == 0 {
			continue
		}

		var nw *net.IPNet

		_, nw, err = net.ParseCIDR(config[0].Subnet)
		if err != nil {
			return nil, errors.Wrap(err, "parse docker network cidr")
		}

		sz, _ := nw.Mask.Size()

		set[nw.String()] = sz
	}

	return set, nil
}

func createSubnetRange(cidr string) (*ipnet.SubnetRange, error) {
	subnet, err := ipnet.NewSubnetRage(
		cidr, func(addr net.IP) bool {
			if lb := addr.To4()[3]; lb > 2 && lb < 254 {
				return true
			}

			return false
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "generate subnet range")
	}

	return subnet, nil
}

func makeContainerConfig(c containers.Container) *types.ContainerCreateConfig {
	// настраиваем контейнер (основные параметры)
	opts := &types.ContainerCreateConfig{
		Name: c.GetName(),
		Config: &container.Config{
			Image:        c.GetImage(),
			Hostname:     c.GetName(),
			Env:          c.GetEnvs(),
			ExposedPorts: sliceToDockerPortSet(c.ContainerPorts()),
			Volumes:      containers.SliceToSet(c.GetVolumes()),
		},
		HostConfig: &container.HostConfig{
			Mounts:       sliceToDockerMounts(c.GetMounts()),
			NetworkMode:  "bridge",
			PortBindings: portMapToDocker(c.PortMap()),
			Sysctls:      c.GetSysctls(),
			AutoRemove:   c.GetAutoremove(),
		},
	}

	if entrypoint := c.GetEntryPoint(); entrypoint != "" {
		opts.Config.Entrypoint = strings.Split(entrypoint, " ")
	}

	if cmds := c.GetCmd(); len(cmds) != 0 {
		opts.Config.Cmd = append(opts.Config.Cmd, cmds...)
	}

	// настраиваем соединение с сетью контейнера
	opts.NetworkingConfig = &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			c.GetNetwork().Name(): {NetworkID: c.GetNetwork().ID()},
		},
	}

	if contIP := c.GetContainerIP(); contIP != "" {
		opts.NetworkingConfig.EndpointsConfig[c.GetNetwork().Name()].IPAMConfig = &network.EndpointIPAMConfig{
			IPv4Address: contIP,
		}
		opts.NetworkingConfig.EndpointsConfig[c.GetNetwork().Name()].IPAddress = contIP
	}

	return opts
}

func sliceToDockerPortSet(slice []containers.Port) nat.PortSet {
	ports := make(nat.PortSet)

	for i := 0; i < len(slice); i++ {
		ports[nat.Port(slice[i])] = struct{}{}
	}

	return ports
}

func portMapToDocker(in containers.PortMap) nat.PortMap {
	pm := make(nat.PortMap)

	for port, bindings := range in {
		pm[nat.Port(port)] = []nat.PortBinding{
			nat.PortBinding(bindings[0]),
		}
	}

	return pm
}

// nolint
func sliceToDockerMounts(slice []string) []mount.Mount {
	var mounts []mount.Mount
	for _, m := range slice {
		mnt := strings.Split(m, ":")
		if len(mnt) == 2 {
			src, dst := mnt[0], mnt[1]
			mounts = append(mounts, mount.Mount{
				Source:   src,
				Target:   dst,
				Type:     mount.TypeBind,
				ReadOnly: false,
			})
		}
	}

	return mounts
}

func inContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	return false
}
