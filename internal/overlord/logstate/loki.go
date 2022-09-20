package logstate

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"

	"github.com/canonical/pebble/internal/plan"
)

func init() {
	RegisterLogBackend("loki", func(t *plan.LogTarget) (LogBackend, error) { return NewLokiBackend(t.Location) })
}

type LokiBackend struct {
	address *url.URL
}

func NewLokiBackend(address string) (LogBackend, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		u, err = url.Parse("//" + address)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid loki server address: %v", err)
	} else if u.Scheme != "" && u.Scheme != "http" {
		return nil, fmt.Errorf("unsupported loki address scheme '%v'", u.Scheme)
	} else if u.RequestURI() != "" && u.RequestURI() != "/" {
		return nil, fmt.Errorf("invalid loki address: extraneous path %q", u.RequestURI())
	}

	// check for and set loki defaults if ommitted from address
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Port() == "" {
		u.Host += ":3100"
	}
	u.Path = "/loki/api/v1/push"

	b := &LokiBackend{address: u}
	return b, nil
}

func (b *LokiBackend) Close() error { return nil }

func (b *LokiBackend) Send(m *LogMessage) error {
	data, err := json.Marshal(newLokiMessage(m))
	if err != nil {
		return fmt.Errorf("failed to build loki message: %v", err)
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	_, err = gzWriter.Write(data)
	if err != nil {
		return fmt.Errorf("failed to compress loki message: %v", err)
	}
	err = gzWriter.Close()
	if err != nil {
		return fmt.Errorf("failed to compress loki message: %v", err)
	}

	r, err := http.NewRequest("POST", b.address.String(), &buf)
	if err != nil {
		return fmt.Errorf("failed to build loki message request: %v", err)
	}
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("Content-Encoding", "gzip")
	c := &http.Client{}
	resp, err := c.Do(r)
	if err != nil {
		return fmt.Errorf("failed to send loki message: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, err = ioutil.ReadAll(resp.Body)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to send loki message: %s", data)
		}
	}
	return nil
}

type lokiMessageStream struct {
	Stream map[string]string `json:"stream"` // TODO: use this for future log labels
	Values [][2]string       `json:"values"`
}

type lokiMessage struct {
	Streams []lokiMessageStream `json:"streams"`
}

func newLokiMessage(msg *LogMessage) *lokiMessage {
	timestamp := strconv.FormatInt(msg.Timestamp.UnixNano(), 10)
	return &lokiMessage{
		Streams: []lokiMessageStream{
			lokiMessageStream{
				Values: [][2]string{{timestamp, string(msg.Message)}},
				Stream: map[string]string{"service": msg.Service},
			},
		},
	}
}
