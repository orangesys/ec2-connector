package loki

import (
	"path"
	"runtime"
	"strings"

	"github.com/rs/zerolog"
)

func NewLog() zerolog.Logger {
	labels := map[string]string{
		"instance": "test",
	}
	return zerolog.New(New(labels)).With().Timestamp().Logger()
}

func NewWithExternalLables(labels map[string]string) zerolog.Logger {
	for k, v := range map[string]string{
		"instance": "test",
	} {
		labels[k] = v
	}
	return zerolog.New(New(labels)).With().Timestamp().Logger()
}

func funcName() string {
	_, file, _, _ := runtime.Caller(2)
	dir, _ := path.Split(file)
	parts := strings.Split(dir, "/")
	pl := len(parts)
	return parts[pl-2]
}
