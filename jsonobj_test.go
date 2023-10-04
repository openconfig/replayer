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

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestIntField(t *testing.T) {
	obj := jsonObj{
		"intKey": 5,
		"strKey": "strVal",
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
		"strKey": "strVal",
		"intKey": 0,
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
		"objKey": objVal,
		"mapKey": mapVal,
		"intKey": 0,
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
		"objListKey": objListVal,
		"mapListKey": []map[string]any{objVal},
		"anyListKey": []any{objVal},
		"intElemKey": []any{5},
		"intKey":     0,
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
