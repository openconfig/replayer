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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mohae/deepcopy"
)

func TestLookupIgnorePrefix(t *testing.T) {
	obj := jsonObj{
		"key":         1,
		"prefix:key2": 2,
	}

	tests := []struct {
		desc   string
		key    string
		want   any
		wantOk bool
	}{
		{
			desc:   "present",
			key:    "key",
			want:   1,
			wantOk: true,
		},
		{
			desc: "absent",
			key:  "absentKey",
		},
		{
			desc:   "prefixed",
			key:    "key2",
			want:   2,
			wantOk: true,
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, gotOk := obj.lookupIgnorePrefix(test.key)
			if gotOk != test.wantOk {
				t.Errorf("lookupIgnorePrefix(%q) got unexpected error %v, want %v", test.key, gotOk, test.wantOk)
			}
			if !cmp.Equal(test.want, got) {
				t.Errorf("lookupIgnorePrefix(%q) got %v, want %v", test.key, got, test.want)
			}
		})
	}
}

func TestIntField(t *testing.T) {
	obj := jsonObj{
		"intKey":             5,
		"strKey":             "strVal",
		"prefix:prefixedInt": 6,
	}

	tests := []struct {
		desc    string
		key     string
		want    int
		wantErr string
	}{{
		desc: "present",
		key:  "intKey",
		want: 5,
	}, {
		desc: "absent",
		key:  "absentKey",
	}, {
		desc: "prefixed",
		key:  "prefixedInt",
		want: 6,
	}, {
		desc:    "wrong type",
		key:     "strKey",
		wantErr: "not an integer",
	}}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := obj.intField(test.key)
			if (err == nil) != (test.wantErr == "") || err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("intField(%q) got unexpected error %v, want containing %q", test.key, err, test.wantErr)
			}
			if got != test.want {
				t.Errorf("intField(%q) got %q, want %q", test.key, got, test.want)
			}
		})
	}
}

func TestStringField(t *testing.T) {
	obj := jsonObj{
		"strKey":             "strVal",
		"intKey":             0,
		"prefix:prefixedStr": "prefixedStrVal",
	}

	tests := []struct {
		desc    string
		key     string
		want    string
		wantErr string
	}{{
		desc: "present",
		key:  "strKey",
		want: "strVal",
	}, {
		desc: "absent",
		key:  "absentKey",
	}, {
		desc: "prefixed",
		key:  "prefixedStr",
		want: "prefixedStrVal",
	}, {
		desc:    "wrong type",
		key:     "intKey",
		wantErr: "not a string",
	}}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := obj.stringField(test.key)
			if (err == nil) != (test.wantErr == "") || err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("stringField(%q) got unexpected error %v, want containing %q", test.key, err, test.wantErr)
			}
			if got != test.want {
				t.Errorf("stringField(%q) got %q, want %q", test.key, got, test.want)
			}
		})
	}
}

func TestObjectField(t *testing.T) {
	objVal := jsonObj{"isJsonObj": true}
	mapVal := map[string]any{"isMap": true}
	obj := jsonObj{
		"objKey":                objVal,
		"mapKey":                mapVal,
		"intKey":                0,
		"prefix:prefixedObject": objVal,
	}

	tests := []struct {
		desc    string
		key     string
		want    jsonObj
		wantErr string
	}{{
		desc: "object",
		key:  "objKey",
		want: objVal,
	}, {
		desc: "map",
		key:  "mapKey",
		want: mapVal,
	}, {
		desc: "absent",
		key:  "absentKey",
	}, {
		desc: "prefixed",
		key:  "prefixedObject",
		want: objVal,
	}, {
		desc:    "wrong type",
		key:     "intKey",
		wantErr: "not an object",
	}}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := obj.objectField(test.key)
			if (err == nil) != (test.wantErr == "") || err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("objectField(%q) got unexpected error %v, want containing %q", test.key, err, test.wantErr)
			}
			if !cmp.Equal(got, test.want) {
				t.Errorf("objectField(%q) got %q, want %q", test.key, got, test.want)
			}
		})
	}
}

func TestObjectListField(t *testing.T) {
	objVal := jsonObj{"isJsonObj": true}
	objListVal := []jsonObj{objVal}
	obj := jsonObj{
		"objListKey":          objListVal,
		"mapListKey":          []map[string]any{objVal},
		"anyListKey":          []any{objVal},
		"intElemKey":          []any{5},
		"intKey":              0,
		"prefix:prefixedList": []jsonObj{objVal},
	}

	tests := []struct {
		desc    string
		key     string
		want    []jsonObj
		wantErr string
	}{{
		desc: "object list",
		key:  "objListKey",
		want: objListVal,
	}, {
		desc: "map list",
		key:  "mapListKey",
		want: objListVal,
	}, {
		desc: "any list",
		key:  "anyListKey",
		want: objListVal,
	}, {
		desc: "absent",
		key:  "absentKey",
	}, {
		desc: "prefixed list",
		key:  "prefixedList",
		want: objListVal,
	}, {
		desc:    "wrong type",
		key:     "intKey",
		wantErr: "not a list",
	}, {
		desc:    "wrong elem type",
		key:     "intElemKey",
		wantErr: "not an object",
	}}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := obj.objectListField(test.key)
			if (err == nil) != (test.wantErr == "") || err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("objectListField(%q) got unexpected error %v, want containing %q", test.key, err, test.wantErr)
			}
			if !cmp.Equal(got, test.want) {
				t.Errorf("objectListField(%q) got %q, want %q", test.key, got, test.want)
			}
		})
	}
}

func TestStringListField(t *testing.T) {
	strListVal := []string{"a", "b"}
	anyListVal := []any{"c", "d"}
	obj := jsonObj{
		"strListKey": strListVal,
		"anyListKey": anyListVal,
		"intElemKey": []any{5},
		"intKey":     0,
	}

	tests := []struct {
		desc    string
		key     string
		want    []string
		wantErr string
	}{{
		desc: "string list",
		key:  "strListKey",
		want: strListVal,
	}, {
		desc: "any list",
		key:  "anyListKey",
		want: []string{"c", "d"},
	}, {
		desc: "absent",
		key:  "absentKey",
	}, {
		desc:    "wrong type",
		key:     "intKey",
		wantErr: "not a list",
	}, {
		desc:    "wrong elem type",
		key:     "intElemKey",
		wantErr: "not a string",
	}}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := obj.stringListField(test.key)
			if (err == nil) != (test.wantErr == "") || err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("stringListField(%q) got unexpected error %v, want containing %q", test.key, err, test.wantErr)
			}
			if !cmp.Equal(got, test.want) {
				t.Errorf("stringListField(%q) got %q, want %q", test.key, got, test.want)
			}
		})
	}
}

func TestFilterObjectList(t *testing.T) {
	testObj := jsonObj{
		"list": []jsonObj{
			jsonObj{"foo": 1, "keepMe": 1},
			jsonObj{"foo": 2, "keepMe": 0},
			jsonObj{"foo": 3, "keepMe": 1},
		},
		"notList": 123,
	}

	tests := []struct {
		desc    string
		key     string
		filter  func(jsonObj) bool
		want    jsonObj
		wantErr bool
	}{
		{
			desc: "filter everything",
			key:  "list",
			filter: func(jsonObj) bool {
				return false
			},
			want: jsonObj{
				"list":    []jsonObj{},
				"notList": 123,
			},
		},
		{
			desc: "filter nothing",
			key:  "list",
			filter: func(jsonObj) bool {
				return true
			},
			want: jsonObj{
				"list": []jsonObj{
					jsonObj{"foo": 1, "keepMe": 1},
					jsonObj{"foo": 2, "keepMe": 0},
					jsonObj{"foo": 3, "keepMe": 1},
				},
				"notList": 123,
			},
		},
		{
			desc: "filter on field",
			key:  "list",
			filter: func(j jsonObj) bool {
				km, _ := j.intField("keepMe")
				return km == 1
			},
			want: jsonObj{
				"list": []jsonObj{
					jsonObj{"foo": 1, "keepMe": 1},
					jsonObj{"foo": 3, "keepMe": 1},
				},
				"notList": 123,
			},
		},
		{
			desc: "invalid list key",
			key:  "notList",
			filter: func(jsonObj) bool {
				return true
			},
			want:    deepcopy.Iface(testObj).(jsonObj),
			wantErr: true,
		},
		{
			desc: "missing key",
			key:  "notThere",
			filter: func(jsonObj) bool {
				return true
			},
			want: deepcopy.Iface(testObj).(jsonObj),
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			obj := deepcopy.Copy(testObj).(jsonObj)
			err := obj.filterObjectList(test.key, test.filter)
			if (err != nil) != test.wantErr {
				t.Errorf("filterObjectList(%q) got unexpected error %v, want %v", test.desc, err, test.wantErr)
			}
			if diff := cmp.Diff(test.want, obj); diff != "" {
				t.Errorf("filterObjectList(%q) returned unexpected diff (-want +got):\n%s", test.desc, diff)
			}

		})
	}
}

func TestObjectAtPathString(t *testing.T) {
	obj := jsonObj{
		"strKey": "foo",
		"objKey": jsonObj{
			"foo": 1,
		},
		"nested": jsonObj{
			"bar": jsonObj{
				"gaga": "googoo",
			},
		},
		"listKey": []jsonObj{
			{
				"name":     "item2",
				"otherKey": "yes",
				"value":    7,
			},
			{
				"name":     "item1",
				"otherKey": "no",
				"value":    10,
			},
			{
				"name":     "item1",
				"otherKey": "yes",
				"value":    20,
			},
		},
	}
	tests := []struct {
		desc    string
		path    string
		want    jsonObj
		wantErr bool
	}{
		{
			desc: "object key",
			path: "/objKey",
			want: jsonObj{
				"foo": 1,
			},
		},
		{
			desc:    "non-object key",
			path:    "/strKey",
			wantErr: true,
		},
		{
			desc: "nested object key",
			path: "/nested/bar",
			want: jsonObj{
				"gaga": "googoo",
			},
		},
		{
			desc: "key into list",
			path: "/listKey[name=item1][otherKey=yes]",
			want: jsonObj{
				"name":     "item1",
				"otherKey": "yes",
				"value":    20,
			},
		},
		{
			desc: "list without given keys",
			path: "/listKey[name=nono][otherKey=nonono]",
		},
		{
			desc: "no path",
			path: "/foo/bar/baz",
		},
		{
			desc:    "invalid path",
			path:    "this is not a valid path",
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := obj.objectAtPathString(test.path)
			if (err != nil) != test.wantErr {
				t.Errorf("objectAtPathString(%q): got error  %v, want %v", test.path, err, test.wantErr)
			}
			if !cmp.Equal(test.want, got) {
				t.Errorf("objectAtPathString(%q): got  %v, want %v", test.path, got, test.want)
			}
		})
	}
}
