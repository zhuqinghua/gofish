//
// SPDX-License-Identifier: BSD-3-Clause
//

package common

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// Entity provides the common basis for all Redfish and Swordfish objects.
type Entity struct {
	// ODataID is the location of the resource.
	ODataID string `json:"@odata.id"`
	// ID uniquely identifies the resource.
	ID string `json:"Id"`
	// Name is the name of the resource or array element.
	Name string `json:"Name"`
	// Client is the REST client interface to the system.
	client Client
	// etag contains the etag header when fetching the object. This is used to
	// control updates to make sure the object has not been modified my a different
	// process between fetching and updating that could cause conflicts.
	etag string
	// Removes surrounding quotes of etag used in If-Match header of PATCH and POST requests.
	// Only use this option to resolve bad vendor implementation where If-Match only matches the unquoted etag string.
	stripEtagQuotes bool
	// DisableEtagMatch when set will skip the If-Match header from PATCH and POST requests.
	//
	// This is a work around for bad vendor implementations where the If-Match header does not work - even with the '*' value
	// and requests are incorrectly denied with an ETag mismatch error.
	disableEtagMatch bool
}

// SetClient sets the API client connection to use for accessing this
// entity.
func (e *Entity) SetClient(c Client) {
	e.client = c
}

// GetClient get the API client connection to use for accessing this
// entity.
func (e *Entity) GetClient() Client {
	return e.client
}

// Set stripEtagQuotes to enable/disable strupping etag quotes
func (e *Entity) StripEtagQuotes(b bool) {
	e.stripEtagQuotes = b
}

// Disable Etag Match header from being sent by the client.
func (e *Entity) DisableEtagMatch(b bool) {
	e.disableEtagMatch = b
}

// Update commits changes to an entity.
func (e *Entity) Update(originalEntity, updatedEntity reflect.Value, allowedUpdates []string) error {
	payload := getPatchPayloadFromUpdate(originalEntity, updatedEntity)

	// See if we are attempting to update anything that is not allowed
	for field := range payload {
		found := false
		for _, name := range allowedUpdates {
			if name == field {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("%s field is read only", field)
		}
	}

	// If there are any allowed updates, try to send updates to the system and
	// return the result.
	if len(payload) > 0 {
		return e.Patch(e.ODataID, payload)
	}

	return nil
}

// Get performs a Get request against the Redfish service and save etag
func (e *Entity) Get(c Client, uri string, payload interface{}) error {
	resp, err := c.Get(uri)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// zhuqh add 2024-01-18
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	type checkStruct struct {
		ID interface{} `json:"id"`
	}
	var check checkStruct
	if err := json.Unmarshal(data, &check); err != nil {
		return err
	}
	switch v := check.ID.(type) {
	case int:
		// 处理 ID 是 int 类型的情况，转换为 string 后再存回结构体
		check.ID = strconv.Itoa(v)
	}
	if jsonData, err := json.Marshal(check); err == nil {
		if err := json.Unmarshal(jsonData, &payload); err != nil {
			return err
		}
	} else {
		return err
	}
	// err = json.NewDecoder(resp.Body).Decode(payload)
	// zhuqh add end
	if err != nil {
		return err
	}

	if resp.Header["Etag"] != nil {
		e.etag = resp.Header["Etag"][0]
	}
	e.SetClient(c)
	return nil
}

// Patch performs a Patch request against the Redfish service with etag
func (e *Entity) Patch(uri string, payload interface{}) error {
	header := make(map[string]string)
	if e.etag != "" && !e.disableEtagMatch {
		if e.stripEtagQuotes {
			e.etag = strings.Trim(e.etag, "\"")
		}

		header["If-Match"] = e.etag
	}

	resp, err := e.client.PatchWithHeaders(uri, payload, header)
	if err == nil {
		return resp.Body.Close()
	}
	return err
}

// Post performs a Post request against the Redfish service with etag
func (e *Entity) Post(uri string, payload interface{}) error {
	header := make(map[string]string)
	if e.etag != "" && !e.disableEtagMatch {
		if e.stripEtagQuotes {
			e.etag = strings.Trim(e.etag, "\"")
		}

		header["If-Match"] = e.etag
	}

	resp, err := e.client.PostWithHeaders(uri, payload, header)
	if err == nil {
		return resp.Body.Close()
	}
	return err
}

type Filter string

type FilterOption func(*Filter)

func WithSkip(skipNum int) FilterOption {
	return func(e *Filter) {
		*e = Filter(fmt.Sprintf("%s$skip=%d", *e, skipNum))
	}
}

func WithTop(topNum int) FilterOption {
	return func(e *Filter) {
		*e = Filter(fmt.Sprintf("%s$top=%d", *e, topNum))
	}
}

func (e *Filter) SetFilter(opts ...FilterOption) {
	*e = "?"
	lastIdx := len(opts) - 1
	for idx, opt := range opts {
		opt(e)
		if idx < lastIdx {
			*e += "&"
		}
	}
}

func (e *Filter) ClearFilter() {
	*e = ""
}

func getPatchPayloadFromUpdate(originalEntity, updatedEntity reflect.Value) (payload map[string]interface{}) {
	payload = make(map[string]interface{})

	for i := 0; i < originalEntity.NumField(); i++ {
		if !originalEntity.Field(i).CanInterface() {
			// Private field or something that we can't access
			continue
		}
		field := originalEntity.Type().Field(i)
		if field.Type.Kind() == reflect.Ptr {
			continue
		}
		fieldName := field.Name
		jsonName := field.Tag.Get("json")
		if jsonName == "-" {
			continue
		}
		if jsonName != "" {
			fieldName = jsonName
		}
		originalValue := originalEntity.Field(i).Interface()
		currentValue := updatedEntity.Field(i).Interface()
		if originalValue == nil && currentValue == nil {
			continue
		}

		if originalValue == nil {
			payload[fieldName] = currentValue
			continue
		}

		switch reflect.TypeOf(originalValue).Kind() {
		case reflect.Slice, reflect.Map:
			if !reflect.DeepEqual(originalValue, currentValue) {
				payload[fieldName] = currentValue
			}
		case reflect.Struct:
			structPayload := getPatchPayloadFromUpdate(originalEntity.Field(i), updatedEntity.Field(i))
			if field.Anonymous {
				for k, v := range structPayload {
					payload[k] = v
				}
			} else if len(structPayload) != 0 {
				payload[fieldName] = structPayload
			}
		default:
			if originalValue != currentValue {
				payload[fieldName] = currentValue
			}
		}
	}
	return payload
}
