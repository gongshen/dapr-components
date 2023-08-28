package influxagent

import (
	"context"
	"fmt"
	contribMetadata "github.com/dapr/components-contrib/metadata"
	mdutils "github.com/dapr/components-contrib/metadata"
	"github.com/dapr/components-contrib/middleware"
	"github.com/dapr/kit/logger"
	"net"
	"net/http"
	"reflect"
	"time"
)

type statusCodeWriter struct {
	http.ResponseWriter
	statusCode int
}

func (sw *statusCodeWriter) WriteHeader(statusCode int) {
	sw.statusCode = statusCode
	sw.ResponseWriter.WriteHeader(statusCode)
}

// Metadata is the influx-agent middleware config.
type influxAgentMiddlewareMetadata struct {
	ServiceName string `json:"servicename"`
	Address     string `json:"address"`
	Database    string `json:"database"`
}

// NewInfluxAgentMiddleware returns a new influx-agent middleware.
func NewInfluxAgentMiddleware(log logger.Logger) middleware.Middleware {
	m := &Middleware{logger: log}
	return m
}

// Middleware is an oAuth2 authentication middleware.
type Middleware struct {
	logger logger.Logger
	conn   net.Conn
	*influxAgentMiddlewareMetadata
}

// GetHandler returns the HTTP handler provided by the middleware.
func (m *Middleware) GetHandler(_ context.Context, metadata middleware.Metadata) (func(next http.Handler) http.Handler, error) {
	var middlewareMetadata influxAgentMiddlewareMetadata
	err := mdutils.DecodeMetadata(metadata.Properties, &middlewareMetadata)
	if err != nil {
		return nil, err
	}
	if middlewareMetadata.Address == "" || middlewareMetadata.Database == "" {
		return nil, nil
	}
	m.conn, err = net.Dial("udp", middlewareMetadata.Address)
	if err != nil {
		return nil, err
	}
	m.influxAgentMiddlewareMetadata = &middlewareMetadata
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			start := time.Now()
			url := request.URL
			w := &statusCodeWriter{ResponseWriter: writer}
			next.ServeHTTP(w, request)
			reqTime := fmt.Sprintf("%.3f", float32(time.Since(start).Milliseconds())/float32(1000))
			str := fmt.Sprintf("<url> %s %s %s,%d,%s,%s,%s,%s", m.Database, m.ServiceName, url.EscapedPath(), w.statusCode, reqTime, "-", "-", "-")
			_, _ = m.conn.Write([]byte(str))
		})
	}, nil
}

func (m *Middleware) Close() error {
	m.conn.Close()
	return nil
}

func (m *Middleware) GetComponentMetadata() map[string]string {
	metadataStruct := influxAgentMiddlewareMetadata{}
	metadataInfo := map[string]string{}
	contribMetadata.GetMetadataInfoFromStructType(reflect.TypeOf(metadataStruct), &metadataInfo, contribMetadata.MiddlewareType)
	return metadataInfo
}
