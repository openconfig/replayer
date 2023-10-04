// Copyright 2023 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package replayer

import "fmt"

// jsonObj represents a JSON object.
type jsonObj map[string]any

// intField looks up a integer field of a json object by its key.
// If there is no field with that key, zero is returned.
// If there is such a field but it is not a string, an error is returned.
func (j jsonObj) intField(key string) (int, error) {
	val, ok := j[key]
	if !ok {
		return 0, nil
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	}
	return 0, fmt.Errorf("field %q has value %v of type %T, not an integer", key, val, val)
}

// stringField looks up a string field of a json object by its key.
// If there is no field with that key, the empty string is returned.
// If there is such a field but it is not a string, an error is returned.
func (j jsonObj) stringField(key string) (string, error) {
	val, ok := j[key]
	if !ok {
		return "", nil
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("field %q has value %v of type %T, not a string", key, val, val)
	}
	return str, nil
}

// objectField looks up an object field of a json object by its key.
// If there is no field with that key in the object, the returned object is nil.
// If there is such a field but it is not an object, an error is returned.
func (j jsonObj) objectField(key string) (jsonObj, error) {
	val, ok := j[key]
	if !ok {
		return nil, nil
	}
	switch v := val.(type) {
	case jsonObj:
		return v, nil
	case map[string]any:
		return v, nil
	}
	return nil, fmt.Errorf("field %q has value %v of type %T, not an object", key, val, val)
}

// objectListField looks up an object list field of a json object by its key.
// If there is no field with that key in the object, the returned list is nil.
// If there is such a field but it is not an object list, an error is returned.
func (j jsonObj) objectListField(key string) ([]jsonObj, error) {
	val, ok := j[key]
	if !ok {
		return nil, nil
	}
	switch v := val.(type) {
	case []jsonObj:
		return v, nil
	case []map[string]any:
		var objList []jsonObj
		for _, elem := range v {
			objList = append(objList, elem)
		}
		return objList, nil
	case []any:
		var objList []jsonObj
		for _, elem := range v {
			switch e := elem.(type) {
			case jsonObj:
				objList = append(objList, e)
			case map[string]any:
				objList = append(objList, e)
			default:
				return nil, fmt.Errorf("field %q has element %v of type %T, not an object", key, elem, elem)
			}
		}
		return objList, nil
	}
	return nil, fmt.Errorf("field %q has value %v of type %T, not a list", key, val, val)
}
