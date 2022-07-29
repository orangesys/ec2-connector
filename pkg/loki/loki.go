package loki

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Interface interface {
	io.Writer
}

type stream struct {
	Labels map[string]string `json:"stream"`
	Lines  [][2]string       `json:"values"`
}

func newStream(labels map[string]string, log string) stream {
	return stream{
		Labels: labels,
		Lines:  [][2]string{{strconv.FormatInt(time.Now().UnixNano(), 10), log}},
	}
}

type Loki struct {
	labels map[string]string
}

func New(labels map[string]string) *Loki {
	return &Loki{
		labels: labels,
	}
}

func (l *Loki) Write(p []byte) (n int, err error) {
	var streams struct {
		Streams []stream `json:"streams"`
	}
	streams.Streams = []stream{newStream(l.labels, fmt.Sprintf("%s", p))}
	content, _ := json.Marshal(streams)
	resp, err := http.Post(os.Getenv("LOKI_URL")+"/loki/api/v1/push", "application/json", bytes.NewReader(content))
	if err != nil {
		fmt.Printf("%s\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return 0, err
		}
		fmt.Printf("code:%d err:%s \n", resp.StatusCode, string(body))
		return 0, errors.New(string(body))
	}
	return
}
