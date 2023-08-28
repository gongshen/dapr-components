package etcd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/dapr/kit/config"
)

const (
	defaultDaprPrefix = "root/qware/dapr/nameresolution"
)

type configSpec struct {
	Endpoints            []string
	DialTimeout          int64
	DialKeepAliveTime    int64
	DialKeepAliveTimeout int64
	TTL                  int64
	RegisterPrefix       string
	Namespace            string
	Picker               string
	DeregisterAddr       string
}

func parseConfig(rawConfig interface{}) (*configSpec, error) {
	result := new(configSpec)
	rawConfig, err := config.Normalize(rawConfig)
	if err != nil {
		return result, err
	}
	data, err := json.Marshal(rawConfig)
	if err != nil {
		return result, fmt.Errorf("error serializing to json: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(result); err != nil {
		return result, fmt.Errorf("error deserializing to configSpec: %w", err)
	}
	if result.DialTimeout <= 0 {
		result.DialTimeout = 3
	}
	if result.DialKeepAliveTime <= 0 {
		result.DialKeepAliveTime = 3
	}
	if result.DialKeepAliveTimeout <= 0 {
		result.DialKeepAliveTimeout = 1
	}
	if result.TTL <= 0 {
		result.TTL = result.DialKeepAliveTime*2 + 1
	}
	if result.RegisterPrefix == "" {
		result.RegisterPrefix = defaultDaprPrefix
	}
	return result, nil
}
