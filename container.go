package containers

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"git.corout.in/golibs/confctl"
	"git.corout.in/golibs/errors"
	"git.corout.in/golibs/errors/errgroup"
	"git.corout.in/golibs/network/ports"
)

// Общие настройки контейнера
const (
	ErrContainerExitedBeforeReady = errors.Const("container exited before ready")
	ErrContainerAlreadyStoped     = errors.Const("container already stopped")
	ErrContainerDidntStart        = errors.Const("container did not start")
	StartTimeoutFactorEnvar       = "DEBUG_START_TIMEOUT_FACTOR"
)

var _ Container = (*BaseContainer)(nil)

type ContainerInfo struct {
	ID        string
	IPAddress string
	PortBinds PortMap
	Networks  map[string]EndpointSettings
}

type ContainerStatus struct {
	StatusCode int64
	Error      error
}

// BaseContainer - базовый тип обертки над нативным docker container
// nolint:maligned
type BaseContainer struct {
	Ctx          context.Context
	Ready        ReadyFunc
	OutputStream io.Writer
	ErrorStream  io.Writer

	Name        string
	TypeID      uint8
	Image       string
	EntryPoint  string
	client      Client
	network     Network
	containerID string

	hostIP      string
	ContainerIP string

	Cmd       []string
	Mounts    []string
	Envs      []string
	Volumes   []string
	Sysctls   map[string]string
	DebugPort ports.DebugPort
	Ports     PortBinds
	portnames map[string]ports.PortName

	StartTimeout time.Duration
	Autoremove   bool
	NotBindPorts bool
	Background   bool

	containerAddress AddrsMap
	hostAddress      AddrsMap

	ConfController controllers.Controller

	mutex   sync.Mutex
	stopped bool
}

// NewBaseContainer - конструктор базового контейнера
func NewBaseContainer(cli Client, nw Network, confCtl controllers.Controller) *BaseContainer {
	cont := &BaseContainer{
		client:         cli,
		ConfController: confCtl,
		Sysctls: map[string]string{
			"net.ipv4.ip_forward": "1",
		},
		network:          nw,
		portnames:        make(map[string]ports.PortName),
		containerAddress: make(AddrsMap),
		hostAddress:      make(AddrsMap),
		hostIP:           nw.HostIP(),
	}

	return cont
}

// GetID - возвращает идентификатор запущенного контейнера после его создания
func (c *BaseContainer) GetID() string {
	return c.containerID
}

// HostIP - возвращает IP хоста ссети для текущего контейнера
func (c *BaseContainer) HostIP() string {
	return c.hostIP
}

func (c *BaseContainer) GetContainerIP() string {
	if c != nil {
		return c.ContainerIP
	}

	return ""
}

// HostPort - возвращает фактический порт по имени порта
func (c *BaseContainer) HostPort(name ports.PortName) string {
	for _, b := range c.Ports {
		if b.Name == name {
			return strconv.Itoa(int(b.Host))
		}
	}

	return ""
}

func (c *BaseContainer) ContainerPorts() []Port {
	ports := make([]Port, len(c.Ports))

	for i := 0; i < len(c.Ports); i++ {
		ports[i] = c.Ports[i].Container
	}

	return ports
}

func (c *BaseContainer) PortMap() PortMap {
	pm := make(PortMap)

	for i := 0; i < len(c.Ports); i++ {
		p := c.Ports[i].Container
		pm[p] = []PortBinding{
			{
				HostIP:   c.hostIP,
				HostPort: strconv.Itoa(int(c.Ports[i].Host)),
			},
		}
	}

	return pm
}

// GetName - возвращает имя контейнера
func (c *BaseContainer) GetName() string {
	return c.Name
}

func (c *BaseContainer) GetImage() string {
	return c.Image
}

func (c *BaseContainer) GetSysctls() map[string]string {
	return c.Sysctls
}

func (c *BaseContainer) GetEnvs() []string {
	return c.Envs
}

func (c *BaseContainer) GetEntryPoint() string {
	return c.EntryPoint
}

func (c *BaseContainer) GetCmd() []string {
	return c.Cmd
}

func (c *BaseContainer) GetVolumes() []string {
	if c != nil {
		return c.Volumes
	}

	return nil
}

func (c *BaseContainer) GetMounts() []string {
	if c != nil {
		return c.Mounts
	}

	return nil
}

func (c *BaseContainer) GetAutoremove() bool {
	if c != nil {
		return c.Autoremove
	}

	return false
}

func (c *BaseContainer) GetNetwork() Network {
	if c != nil {
		return c.network
	}

	return nil
}

// HostAddrs возвращает список эндпоинтов хоста
func (c *BaseContainer) HostAddrs() AddrsMap {
	return c.hostAddress
}

// ContainerAddrs возвращает список эндпоинтов контейнера
func (c *BaseContainer) ContainerAddrs() AddrsMap {
	return c.containerAddress
}

// CreateContainer конфигурирует и создает контейнер
func (c *BaseContainer) CreateContainer() error {
	if c.Ctx == nil {
		c.Ctx = context.Background()
	}

	if c.Ready == nil {
		c.Ready = c.ready
	}

	c.portnames = c.Ports.Names()

	// включение отладки
	c.setupDebug()

	id, err := c.client.ContainerCreate(c.Ctx, c)
	if err != nil {
		return errors.Wrap(err, "create container")
	}

	c.containerID = id

	return nil
}

// StartContainer непосредственно запускает контейнер
func (c *BaseContainer) StartContainer(sigCh <-chan os.Signal, ready chan<- struct{}) error {
	if c.DebugPort.Enabled() {
		c.LogStdout("\n!!! RUNNING IN DEBUG MODE!!! PORT: %s\n\n", c.DebugPort)
	}

	info, err := c.client.ContainerStart(c.Ctx, c.containerID, c.Name)
	if err != nil {
		return errors.Wrapf(err, "start container")
	}

	// заполняем хостовые эндпоинты контейнера
	for _, bind := range info.PortBinds {
		if len(bind) > 0 {
			b := bind[0]
			c.hostAddress[c.portnames[b.HostPort]] = net.JoinHostPort(c.hostIP, b.HostPort)
		}
	}

	// заполняем внутренние эндпоинты контейнера (которые в сети докера)
	containerIP := info.IPAddress

	if containerIP == "" {
		if endpoint, ok := info.Networks[c.network.Name()]; ok {
			containerIP = endpoint.IPAddress
		}
	}

	c.ContainerIP = containerIP

	for _, p := range c.Ports {
		c.containerAddress[p.Name] = net.JoinHostPort(containerIP, p.Container.Port())
	}

	logContext, cancelLogs := context.WithCancel(context.Background())
	defer cancelLogs()

	leg := errgroup.New()
	leg.Go(func() error {
		return c.client.StreamLogs(
			logContext,
			c.containerID,
			c.ErrorStream,
			c.OutputStream,
			true,
		)
	})

	go func() {
		if errList := errors.AsChain(leg.Wait()); len(errList) != 0 {
			for i := 0; i < len(errList); i++ {
				c.LogError(err)
			}
		}
	}()

	c.network.AddContainer(&OrchestratorInfo{
		ID:                info.ID,
		TypeID:            c.TypeID,
		ContainerEnpoints: c.containerAddress,
		HostEnpoints:      c.hostAddress,
	})

	containerExit := c.wait()
	ctx, cancel := context.WithTimeout(context.Background(), c.StartTimeout)

	defer cancel()

	select {
	case <-ctx.Done():
		if stopErr := c.Stop(); err != nil {
			c.LogError(stopErr, "stop container")
		}

		return errors.Ctx().
			Str("container-name", c.GetName()).
			Str("container-id", c.containerID[:12]).
			Just(ErrContainerDidntStart)
	case <-containerExit:
		return ErrContainerExitedBeforeReady
	case <-c.Ready(ctx):
		if !c.LogStdout(c.GetName() + " component ready") {
			_, _ = fmt.Fprintln(os.Stdout, c.GetName()+" component ready")
		}
	}

	if ready != nil {
		close(ready)
	}

	if !c.Background {
		for {
			select {
			case err = <-containerExit:
				return err
			case <-sigCh:
				return c.Stop()
			}
		}
	}

	return nil
}

// Stop останавливает контейнер
func (c *BaseContainer) Stop() error {
	c.mutex.Lock()
	if c.stopped {
		c.mutex.Unlock()

		return errors.Ctx().
			Str("container-id", c.containerID).
			Str("container-name", c.GetName()).
			Just(ErrContainerAlreadyStoped)
	}

	c.stopped = true
	c.mutex.Unlock()

	return c.client.ContainerStop(c.Ctx, c.containerID, time.Duration(0))
}

// LogStdout пишет сообщение во writer потока стандартного вывода контейнера
func (c *BaseContainer) LogStdout(format string, args ...interface{}) bool {
	if c.OutputStream == nil {
		return false
	}

	if _, err := fmt.Fprintf(c.OutputStream, format+"\n", args...); err != nil {
		return false
	}

	return true
}

// LogStderr пишет сообщение во writer потока стандартного вывода ошибок контейнера
func (c *BaseContainer) LogStderr(format string, args ...interface{}) bool {
	if c.ErrorStream == nil {
		return false
	}

	if _, err := fmt.Fprintf(c.ErrorStream, format+"\n", args...); err != nil {
		return false
	}

	return true
}

// LogError пишет ошибку сообщение во writer потока стандартного вывода ошибок контейнера
func (c *BaseContainer) LogError(err error, args ...interface{}) bool {
	return c.LogStderr("\x1b[91mERROR:\x1b[0m " + errors.Formatted(err, args...).Error())
}

func (c *BaseContainer) ready(ctx context.Context) <-chan struct{} {
	readyCh := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second * 5):
			close(readyCh)
			return
		}
	}()

	return readyCh
}

func (c *BaseContainer) setupDebug() {
	if c.DebugPort.Enabled() {
		c.Ports = append(
			c.Ports,
			PortBind{
				Name:      ports.DebugPortName,
				Container: Port(fmt.Sprintf("%d/tcp", c.DebugPort.Port())),
				Host:      ports.BaseDebugPort,
			},
		)

		if strFactor := os.Getenv(StartTimeoutFactorEnvar); strFactor != "" {
			factor, err := strconv.Atoi(strFactor)
			if err != nil {
				c.LogStderr("wrong %s value", StartTimeoutFactorEnvar)
			} else {
				c.StartTimeout = c.StartTimeout * time.Duration(factor)
			}
		}

		c.Cmd = []string{
			"/bin/dlv",
			fmt.Sprintf("--listen=:%d", ports.BaseDebugPort),
			"--headless=true",
			"--api-version=2",
			"--accept-multiclient",
			"exec",
			c.DebugPort.Command(),
		}
	}
}

func (c *BaseContainer) wait() <-chan error {
	exitCh := make(chan error)

	go func() {
		waitCh, errCh := c.client.ContainerWait(context.Background(), c.containerID)

		select {
		case err := <-errCh:
			exitCh <- errors.Ctx().
				Str("container-name", c.GetName()).
				Wrap(err, "container process exited with error")
		case status := <-waitCh:
			exitMsg := fmt.Sprintf("container exited with status: %d", status.StatusCode)
			if status.Error != nil {
				c.LogError(status.Error)
			} else {
				c.LogStdout(exitMsg)
			}

			exitCh <- nil
		}
	}()

	return exitCh
}
