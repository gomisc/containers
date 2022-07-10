package containers

import (
	"strconv"
	"strings"

	"git.corout.in/golibs/network/ports"
)

type (
	Port string

	PortBinding struct {
		HostIP   string
		HostPort string
	}

	PortMap map[Port][]PortBinding

	PortBind struct {
		Name      ports.PortName
		Container Port
		Host      uint16
	}

	PortBinds []PortBind
)

func (p Port) Port() string {
	_, value := splitProtoPort(string(p))

	return value
}

func (p Port) Proto() string {
	proto, _ := splitProtoPort(string(p))
	return proto
}

// Names - возвращает матрицу соответствия портов имени порта
func (pb PortBinds) Names() map[string]ports.PortName {
	names := make(map[string]ports.PortName)

	for _, b := range pb {
		names[b.Container.Port()] = b.Name
		names[strconv.Itoa(int(b.Host))] = b.Name
	}

	return names
}

func splitProtoPort(rawPort string) (string, string) {
	parts := strings.Split(rawPort, "/")
	l := len(parts)
	if len(rawPort) == 0 || l == 0 || len(parts[0]) == 0 {
		return "", ""
	}
	if l == 1 {
		return "tcp", rawPort
	}
	if len(parts[1]) == 0 {
		return "tcp", parts[0]
	}
	return parts[1], parts[0]
}
