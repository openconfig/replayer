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

package internal

import (
	"fmt"
	"strings"

	"github.com/openconfig/ygot/ygot"

	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

// jsonObj represents a JSON object.
type jsonObj map[string]any

// lookupIgnorePrefix will get the value at key ignoring any prefixes and a boolean
// indicating if the key was present in the object. For example, if the json
// object j contains a value v at key 'prefix:foo', getIgnorePrefix("foo")
// returns (v, true).
func (j jsonObj) lookupIgnorePrefix(key string) (any, bool) {
	k, ok := j.keyWithSuffix(key)
	if !ok {
		return nil, false
	}
	v, ok := j[k]
	return v, ok
}

// keyWithSuffix returns the key in an object which has the given suffix,
// including keys equal to the suffix.
func (j jsonObj) keyWithSuffix(suffix string) (string, bool) {
	_, ok := j[suffix]
	if ok {
		return suffix, true
	}

	for k := range j {
		_, keySuffix, ok := strings.Cut(k, ":")
		if !ok {
			continue
		}
		if keySuffix == suffix {
			return k, true
		}
	}
	return "", false
}

// intField looks up a integer field of a json object by its key, ignoring any
// prefixes on the key within the object. If there is no field with that key,
// zero is returned. If there is such a field but it is not a string, an error
// is returned.
func (j jsonObj) intField(key string) (int, error) {
	val, ok := j.lookupIgnorePrefix(key)
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

// stringField looks up a string field of a json object by its key, ignoring any
// prefixes on the key within the object.  If there is no field with that key,
// the empty string is returned.  If there is such a field but it is not a
// string, an error is returned.
func (j jsonObj) stringField(key string) (string, error) {
	val, ok := j.lookupIgnorePrefix(key)
	if !ok {
		return "", nil
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("field %q has value %v of type %T, not a string", key, val, val)
	}
	return str, nil
}

// objectField looks up an object field of a json object by its key, ignoring
// any prefixes on the key within the object.  If there is no field with that
// key in the object, the returned object is nil.  If there is such a field but
// it is not an object, an error is returned.
func (j jsonObj) objectField(key string) (jsonObj, error) {
	val, ok := j.lookupIgnorePrefix(key)
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

// objectListField looks up an object list field of a json object by its key,
// ignoring any prefixes on the key within the object.  If there is no field
// with that key in the object, the returned list is nil.  If there is such a
// field but it is not an object list, an error is returned.
func (j jsonObj) objectListField(key string) ([]jsonObj, error) {
	val, ok := j.lookupIgnorePrefix(key)
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

// stringListField looks up a string list field of a json object by its key.
// If there is no field with that key in the object, the returned list is nil.
// If there is such a field but it is not a string list, an error is returned.
func (j jsonObj) stringListField(key string) ([]string, error) {
	val, ok := j[key]
	if !ok {
		return nil, nil
	}
	switch v := val.(type) {
	case []string:
		return v, nil
	case []any:
		var strList []string
		for _, elem := range v {
			switch e := elem.(type) {
			case string:
				strList = append(strList, e)
			default:
				return nil, fmt.Errorf("field %q has element %v of type %T, not a string", key, elem, elem)
			}
		}
		return strList, nil
	}
	return nil, fmt.Errorf("field %q has value %v of type %T, not a list", key, val, val)
}

// objectAtPath returns the json object found by following the given path. This
// accounts for key prefixes and list node keys.  If there is no field at that
// path, the returned object is nil. If there is such a field but it is not an
// object, an error is returned.
func (j jsonObj) objectAtPath(path *gnmipb.Path) (jsonObj, error) {
	curr := j

	for _, p := range path.Elem {
		if keys := p.GetKey(); len(keys) != 0 {
			// This is a list node. Key into it properly.
			list, err := curr.objectListField(p.GetName())
			if err != nil || list == nil {
				return nil, err
			}

			var found bool
		items:
			for _, item := range list {
				for k, v := range keys {
					if got, _ := item.stringField(k); got != v {
						// Not the item we want
						continue items
					}
				}
				// this is the item we want
				curr = item
				found = true
				break items
			}

			if !found {
				// The list doesn't contain an item with the right keys.
				return nil, nil
			}
		} else {
			// Not a list node, just get it normally
			var err error
			curr, err = curr.objectField(p.GetName())

			if err != nil {
				return nil, err
			}
			if curr == nil {
				break
			}
		}
	}

	return curr, nil
}

// objectAtPathString gets the object at the given OC path, represented by a string.
// The path should be relative to the root of the object. Returns error if the path is
// invalid or the node at this path is not an object. Returns nil if there is no
// node at the path.
func (j jsonObj) objectAtPathString(path string) (jsonObj, error) {
	gnmiPath, err := ygot.StringToStructuredPath(path)
	if err != nil {
		return nil, err
	}
	return j.objectAtPath(gnmiPath)
}

// filterObjectList filters a json object list at key using the filter function.
// The json object is modified in place to replace the old list at the given key
// with the filtered list. Note that if the filtered list contains no items, the
// list value is replaced with an empty list, not a nil list. If the key is not
// found, this is a no-op. The given key ignores prefixes, calling
// filterObjectList with key "foo" will filter either the list at key "foo" or
// the list at key "prefix:foo", in that order of preference.
func (j jsonObj) filterObjectList(key string, filterFn func(jsonObj) bool) error {
	k, ok := j.keyWithSuffix(key)
	if !ok {
		return nil
	}
	list, err := j.objectListField(k)
	if err != nil {
		return err
	}
	if list == nil {
		return nil
	}
	newList := []jsonObj{}
	for _, obj := range list {
		if filterFn(obj) {
			newList = append(newList, obj)
		}
	}
	j[k] = newList
	return nil
}
