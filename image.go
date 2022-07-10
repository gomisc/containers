package containers

import (
	"io"
)

// Images пакет имен образов и опций их подготовки (скачивание, сборка, etc)
type (
	// ImageOption - опция действия при отсутствии указанного образа
	ImageOption func(o *imageOptions)

	// Images пакет имен образов и опций их подготовки (скачивание, сборка, etc)
	Images map[string]ImageOption

	// ImageBuildData - данные необходимые для сборки образа
	ImageBuildData struct {
		Tags       []string
		Args       map[string]*string
		Root       string
		Dockerfile string
		Nocache    bool
		ClearRoot  bool
		Output     io.Writer
	}

	// imageOptions опционал действий при отсутствии указанного докер образа
	imageOptions struct {
		data       *ImageBuildData
		err        error
		forceBuild bool
		pull       bool
	}
)
