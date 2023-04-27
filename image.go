package containers

import (
	"context"
	"io"
	"strings"

	"gopkg.in/gomisc/errors.v1"
)

// Images пакет имен образов и опций их подготовки (скачивание, сборка, etc)
type (
	// ImageOption - опция действия при отсутствии указанного образа
	ImageOption func(o *ImageOptions)

	// Images пакет имен образов и опций их подготовки (скачивание, сборка, etc)
	Images map[string]ImageOption

	// ImageBuildPreparer - колбэк, подготавливающий данные для билдера
	ImageBuildPreparer func() (*ImageBuildData, error)

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

	// ImageOptions опционал действий при отсутствии указанного докер образа
	ImageOptions struct {
		Tags       []string
		Data       *ImageBuildData
		Err        error
		ForceBuild bool
		Pull       bool
	}
)

// WithPullImage - опция скачивания образа при его отсутствии
func WithPullImage(tag string) ImageOption {
	return func(o *ImageOptions) {
		o.Tags = append(o.Tags, tag)
		o.Pull = true
	}
}

// WithBuildImage - опция сборки образа при его отсутствии
func WithBuildImage(preparer ImageBuildPreparer, forceBuild bool) ImageOption {
	return func(o *ImageOptions) {
		o.Data, o.Err = preparer()
		o.ForceBuild = forceBuild
	}
}

func CheckImages(cli Client, opts ...ImageOption) error {
	actions := processImageOptions(opts...)

	for i := 0; i < len(actions); i++ {
		action := actions[i]

		if len(action.Tags) == 0 {
			continue
		}

		if action.Err != nil {
			return errors.Ctx().Strings("tags", action.Tags).
				Wrap(action.Err, "process image")
		}

		exist, err := cli.FindImageLocal(context.Background(), action.Tags[0])
		if err != nil {
			return errors.Ctx().Str("tag", action.Tags[0]).Wrap(err, "find image in local cache")
		}

		if !exist || action.ForceBuild {
			if action.Pull {
				return cli.PullImage(action.Tags[0])
			}

			if action.Data != nil {
				var prevLatest string

				for i := 0; i < len(action.Data.Tags); i++ {
					if strings.Contains(action.Data.Tags[i], ":latest") {
						prevLatest = action.Data.Tags[i]
						break
					}
				}

				if prevLatest != "" {
					cli.RemoveImage(prevLatest)
				}

				if err = cli.BuildImage(action.Data); err != nil {
					return errors.Wrap(err, "build image")
				}

				return nil
			}
		}

	}

	return nil
}

func processImageOptions(opts ...ImageOption) []*ImageOptions {
	actions := make([]*ImageOptions, len(opts))

	for i, apply := range opts {
		actions[i] = &ImageOptions{}
		apply(actions[i])
	}

	return actions
}
