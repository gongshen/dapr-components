/*
Copyright 2021 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package contenttype

import (
	"strings"

	ceproto "github.com/cloudevents/sdk-go/binding/format/protobuf/v2"
	"github.com/cloudevents/sdk-go/v2/event"
)

const (
	// CloudEventContentType is the content type for cloud event.
	CloudEventContentType = "application/cloudevents+json"
	// JSONContentType is the content type for JSON.
	JSONContentType = "application/json"
	// ProtobufContentType is the MIME media type for Protobuf.
	ProtobufContentType = "application/x-protobuf"
	// This is the MIME media type for CloudEvent Protobuf.
	CloudEventProtobufContentType = "application/cloudevents+protobuf"
)

// IsCloudEventContentType checks for content type.
func IsCloudEventContentType(contentType string) bool {
	return isContentType(contentType, CloudEventContentType)
}

// IsJSONContentType checks for content type.
func IsJSONContentType(contentType string) bool {
	return isContentType(contentType, JSONContentType)
}

// IsStringContentType determines if content type is string.
func IsStringContentType(contentType string) bool {
	if strings.HasPrefix(strings.ToLower(contentType), "text/") {
		return true
	}

	return isContentType(contentType, "application/xml")
}

// IsBinaryContentType determines if content type is byte[].
func IsBinaryContentType(contentType string) bool {
	return isContentType(contentType, "application/octet-stream")
}

func isContentType(contentType string, expected string) bool {
	lowerContentType := strings.ToLower(contentType)
	if lowerContentType == expected {
		return true
	}

	semiColonPos := strings.Index(lowerContentType, ";")
	if semiColonPos >= 0 {
		return lowerContentType[0:semiColonPos] == expected
	}

	return false
}

func IsCloudEventProtobuf(contentType string, data []byte) bool {
	if isContentType(contentType, CloudEventProtobufContentType) {
		return true
	}
	// We need to verify that the protobuf is a valid CloudEvent.
	if isContentType(contentType, ProtobufContentType) {
		var e event.Event
		err := ceproto.Protobuf.Unmarshal(data, &e)
		if err != nil {
			return false
		}
		return true
	}
	return false
}
