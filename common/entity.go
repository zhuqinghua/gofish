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
	var check map[string]interface{}
	if err := json.Unmarshal(data, &check); err != nil {
		return err
	}

	if odataId, ok := check["@odata.id"].(string); ok {
		if strings.Contains(odataId, "/redfish/v1/Systems/1/Processors/") {
			if socket, ok := check["Socket"].(float64); ok {
				check["Socket"] = fmt.Sprintf("%v", int(socket))
			}
		}
		if strings.Contains(odataId, "/redfish/v1/Chassis/1/Drives/") {
			if id, ok := check["Id"].(float64); ok {
				check["Id"] = fmt.Sprintf("%v", int(id))
			}
			links, ok := check["Links"].(map[string]interface{})
			if ok {
				volumes, ok := links["Volumes"].(map[string]interface{})
				if ok {
					var sliceData []string
					for _, value := range volumes {
						if oid, ok := value.(string); ok {
							sliceData = append(sliceData, oid)
						}
					}
					delete(links, "Volumes")
					links["Volumes"] = sliceData
				}
			}
		}
		if strings.Contains(odataId, "/redfish/v1/Chassis/1/PCIeDevices/") {
			if id, ok := check["Id"].(float64); ok {
				check["Id"] = fmt.Sprintf("%v", int(id))
			}
		}
		// 这个地方牺牲标准，适配suma服务器，因为suma的值是string无法转int
		if strings.Contains(odataId, "/redfish/v1/Systems/1/Memory/") {
			memLocation, ok := check["MemoryLocation"].(map[string]interface{})
			if ok {
				if socket, ok := memLocation["Socket"].(float64); ok {
					memLocation["Socket"] = fmt.Sprintf("%v", int(socket))
				}
				if channel, ok := memLocation["Channel"].(float64); ok {
					memLocation["Channel"] = fmt.Sprintf("%v", int(channel))
				}
				if slot, ok := memLocation["Slot"].(float64); ok {
					memLocation["Slot"] = fmt.Sprintf("%v", int(slot))
				}
			}
		}
		// CS5280H2服务器controllers结构包一层切片
		if strings.Contains(odataId, "/redfish/v1/Chassis/1/NetworkAdapters/") {
			if controllers, ok := check["Controllers"].(map[string]interface{}); ok {
				sliceData := []map[string]interface{}{controllers}
				delete(check, "Controllers")
				check["Controllers"] = sliceData
			}
		}
		// CS5280H2服务器StorageControllers类型转化，这里很恶心，路径有单词拼写错误（Systems->Systens）
		if strings.Contains(odataId, "/redfish/v1/Systens/1/Storages/") {
			if controllers, ok := check["StorageControllers"].([]map[string]interface{}); ok {
				for _, controller := range controllers {
					if memberID, ok := controller["MemberID"].(float64); ok {
						controller["MemberID"] = fmt.Sprintf("%v", int(memberID))
					}
					if speedGbps, ok := controller["SpeedGbps"].(string); ok {
						result, err := strconv.ParseFloat(speedGbps, 32)
						if err != nil {
							result = 0
						}
						controller["SpeedGbps"] = float32(result)
					}
				}
			}
		}
		// 包一层切片
		if odataId == "/redfish/v1/Managers/1" {
			links, ok := check["Links"].(map[string]interface{})
			if ok {
				managerForChassis, ok := links["ManagerForChassis"].(map[string]interface{})
				if ok {
					sliceData := []map[string]interface{}{managerForChassis}
					delete(links, "ManagerForChassis")
					links["ManagerForChassis"] = sliceData
				}
				managerInChassis, ok := links["ManagerInChassis"].(map[string]interface{})
				if ok {
					sliceData := []map[string]interface{}{managerInChassis}
					delete(links, "ManagerInChassis")
					links["ManagerInChassis"] = sliceData
				}
				managerForServers, ok := links["ManagerForServers"].(map[string]interface{})
				if ok {
					sliceData := []map[string]interface{}{managerForServers}
					delete(links, "ManagerForServers")
					links["ManagerForServers"] = sliceData
				}
			}
		}
	}

	if updatedJSON, err := json.Marshal(check); err == nil {
		if err := json.Unmarshal(updatedJSON, &payload); err != nil {
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
