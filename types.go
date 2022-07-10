package containers

import (
	"context"

	"git.corout.in/golibs/network/ports"
)

type (
	// AddrsMap - мапа адресов для разименования по портам
	AddrsMap map[ports.PortName]string

	// ReadyFunc - обработчик готовности контейнера
	ReadyFunc func(ctx context.Context) <-chan struct{}

	// OrchestratorInfo - информация о контейнере в представлении оркестратора
	OrchestratorInfo struct {
		ID                string   `json:"id"`
		TypeID            uint8    `json:"type_id"`
		ContainerEnpoints AddrsMap `json:"container_enpoints"`
		HostEnpoints      AddrsMap `json:"host_enpoints"`
	}
)
