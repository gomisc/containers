package containers

import (
	"context"
	"io"
	"net"
	"os"
	"time"
)

// Container - интерфейс работы с docker-контейнером
type (
	Container interface {
		// GetClient - возвращает клиента среды исполнения контейнера
		GetClient() Client
		// GetID возвращает идентификатор запущенного контейнера после его создания
		GetID() string
		// GetName возвращает имя контейнера
		GetName() string
		// GetImage возвращает образ
		GetImage() string
		// GetSysctls возвращает данные системных настроек контейнера
		GetSysctls() map[string]string
		// GetContainerIP возвращает внутренний IP адрес контейнера
		GetContainerIP() string
		// ContainerPorts возвращает список внутренних портов контейнера
		ContainerPorts() []Port
		// PortMap возвращает смапленные на хост порты
		PortMap() PortMap
		// GetEnvs возвращает список переменных окружения
		GetEnvs() []string
		// GetEntryPoint возвращает точку схода процесса контейнера
		GetEntryPoint() string
		// GetCmd возвращает команду контейнера
		GetCmd() []string
		// GetVolumes возвращает список разделов
		GetVolumes() []string
		// GetMounts возвращает список подключаемых разделов
		GetMounts() []string
		// GetAutoremove признак авто удаления контейнера после завершения работы
		GetAutoremove() bool
		// GetNetwork возвращает сеть контейнера
		GetNetwork() Network
		// CreateContainer - фаза создания контейнера
		CreateContainer() error
		// StartContainer - фаза старта контейнера
		StartContainer(sigCh <-chan os.Signal, ready chan<- struct{}) error
		// Stop - останавливает контейнер
		Stop() error
		// HostAddrs - возвращает мапу адресов контейнера на хосте
		HostAddrs() AddrsMap
		// ContainerAddrs - возвращает мапу адресов контейнера
		ContainerAddrs() AddrsMap
		// LogStdout пишет сообщение во writer потока стандартного вывода контейнера
		LogStdout(format string, args ...any) bool
		// LogStderr пишет сообщение во writer потока стандартного вывода ошибок контейнера
		LogStderr(format string, args ...any) bool
		// LogError пишет ошибку сообщение во writer потока стандартного вывода ошибок контейнера
		LogError(err error, args ...any) bool
	}

	Client interface {
		// WithStdout устанавливает кастомный поток стандартного вывода
		WithStdout(w io.Writer) Client
		// WithStderr устанавливает кастомный поток вывода ошибок
		WithStderr(w io.Writer) Client
		// IsInContainer - возвращает признак того что процесс сам запущен
		// внутри контейнера
		IsInContainer() bool
		// NetworkList возвращает список сетей
		NetworkList(ctx context.Context) ([]*net.IPNet, error)
		// NextSubnet возвращает адрес следующей незанятой подсети
		NextSubnet() (*net.IPNet, error)
		// RemoveNetwork удаляет пользовательскую сеть
		RemoveNetwork(id string) error
		// ContainerCreate создает контейнер
		ContainerCreate(ctx context.Context, data Container) (string, error)
		// ContainerStart запускает контейнер
		ContainerStart(ctx context.Context, id, name string) (*ContainerInfo, error)
		// ContainerWait ожидает завершения работы контейнера
		ContainerWait(ctx context.Context, id string) (<-chan ContainerStatus, <-chan error)
		// ContainerStop останавливает контейнер
		ContainerStop(ctx context.Context, id string, timeout time.Duration) error
		// StreamLogs подключает вывод логов контейнера
		StreamLogs(ctx context.Context, id string, stderr, stdout io.Writer, follow bool) error
		// FindImageLocal - осуществляет поиск образа в локальном сторе
		FindImageLocal(ctx context.Context, image string) (bool, error)
		// PullImage - скачивает образ в локальный стор
		PullImage(image string) error
		// RemoveImage - удаляет образ из локального стора
		RemoveImage(image string)
		// BuildImage - собирает образ
		BuildImage(data *ImageBuildData) error
		// CheckNetwork проверяет существование сети и создает
		// ее в случае отсутствия
		CheckNetwork(nw, cidr string) (Network, error)
	}

	Network interface {
		// ID возвращает идентификатор сети
		ID() string
		// Name возвращает имя сети
		Name() string
		// Gateway возвращает шлюз сети
		Gateway() string
		// HostIP возвращает адрес хоста
		HostIP() string
		// NextIP возвращает следующий не занятый IP-адрес сети
		NextIP() string
		// AddContainer добавляет данные контейнера
		AddContainer(info *OrchestratorInfo)
	}
)
