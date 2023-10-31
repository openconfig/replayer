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
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/golang/glog"
	"github.com/openconfig/ygot/ygot"
	"google.golang.org/protobuf/proto"

	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

func transformSet(req *gnmipb.SetRequest, r *Recording) (*gnmipb.SetRequest, error) {
	newReq := proto.Clone(req).(*gnmipb.SetRequest)

	// Transform the SetRequest updates.
	newReq.Update = nil
	newReq.Replace = nil
	for _, update := range append(append([]*gnmipb.Update{}, req.GetUpdate()...), req.GetReplace()...) {
		us, err := transformUpdate(update, r)
		if err != nil {
			return nil, err
		}
		if us == nil {
			continue
		}
		newReq.Update = append(newReq.GetUpdate(), us.update...)
		newReq.Replace = append(newReq.GetReplace(), us.replace...)
	}

	// Transform the SetRequest extensions.
	newReq.Extension = nil
	for _, ext := range req.GetExtension() {
		if ext.GetMasterArbitration() != nil {
			continue
		}
		newReq.Extension = append(newReq.Extension, ext)
	}

	return newReq, nil
}

// updates is a new set of gnmi updates produced by transforming and filtering a single update.
type updates struct {
	update  []*gnmipb.Update
	replace []*gnmipb.Update
}

// transformUpdate transforms one update into a new set of updates. These new updates may exclude
// some paths and or values that were present in the original update.
func transformUpdate(update *gnmipb.Update, r *Recording) (*updates, error) {
	if update.GetPath() == nil {
		return nil, nil
	}

	if update.GetPath().GetOrigin() == "cli" {
		// We cannot transform vendor config, so we ignore it.
		return nil, nil
	}

	strpath, err := ygot.PathToString(update.GetPath())
	if err != nil {
		return nil, fmt.Errorf("PathToString(%v): %w", update.GetPath(), err)
	}
	log.Infof("Transforming update for path: %v", strpath)

	// We can match directly on the Update's path for an MVP because we know the exact paths/subtrees
	// that are updated in our samples. For the general case, we would need to account for sub-paths
	// and also maybe traverse the JSON values to reach the subtree we want (e.g. transforming /a/b/c
	// maye apply on Updates for /, /a, /a/b, and /a/b/c, not just /a).
	// TODO(nhawke): Generalize transformation function matching.
	switch strpath {
	case "/interfaces":
		us, err := remapInterfaces(update, r.intfMap)
		if err != nil {
			return nil, err
		}
		if us != nil {
			return &updates{replace: us}, nil
		}
	case "/lacp":
		if err := filterLACP(update); err != nil {
			return nil, err
		}
		return &updates{update: []*gnmipb.Update{update}}, nil
	case "/network-instances":
		return transformNetworkInstances(update)
	case
		"/network-instances/network-instance[name=DECAP]",
		"/network-instances/network-instance[name=REPAIR]",
		"/network-instances/network-instance[name=REPAIRED]",
		"/network-instances/network-instance[name=VRF-blue]",
		"/network-instances/network-instance[name=mgmtVrf]",
		"/network-instances/network-instance[name=default]":
		return transformNetworkInstance(update)
	case
		"/components",
		"/network-instances/network-instance[name=mgmtVrf]/interfaces/interface[id=Management1]",
		"/system/config/hostname",
		"/system/logging",
		"/system/ntp":
		return nil, nil
	}

	// By default, update rather than replace to minimize unintended side effects (e.g. lost connectivity).
	return &updates{update: []*gnmipb.Update{update}}, nil
}

func transformNetworkInstances(update *gnmipb.Update) (*updates, error) {
	ups := &updates{update: []*gnmipb.Update{update}}
	nis, err := unmarshalJSONFromUpdate(update)
	if err != nil {
		return nil, err
	}
	niList, err := nis.objectListField("openconfig-network-instance:network-instance")
	if niList == nil || err != nil {
		return nil, err
	}
	for _, ni := range niList {
		name, err := ni.stringField("name")
		if err != nil {
			return nil, err
		}
		if err := networkInstanceHelper(name, ni, ups); err != nil {
			return nil, err
		}
	}
	if err := marshalJSONToUpdate(update, nis); err != nil {
		return nil, err
	}
	return ups, nil
}

func transformNetworkInstance(update *gnmipb.Update) (*updates, error) {
	ups := &updates{update: []*gnmipb.Update{update}}
	ni, err := unmarshalJSONFromUpdate(update)
	if err != nil {
		return nil, err
	}
	name := update.GetPath().GetElem()[1].GetKey()["name"]
	if err := networkInstanceHelper(name, ni, ups); err != nil {
		return nil, err
	}
	if err := marshalJSONToUpdate(update, ni); err != nil {
		return nil, err
	}
	return ups, nil
}

func networkInstanceHelper(name string, ni jsonObj, ups *updates) error {
	if name == "mgmtVrf" {
		// If the replay needs a mgmtVrf, create an empty placeholder one.
		// This is a no-op on the device if a mgmtVrf already exists.
		for key := range ni {
			delete(ni, key)
		}
		ni["name"] = name
		ni["config"] = jsonObj{
			"name": name,
			"type": "L3VRF",
		}
		return nil
	}

	// Importantly, transforming the protocols must happen before they are split.
	if err := transformProtocols(ni); err != nil {
		return err
	}
	if name == "default" {
		splitUpdates, err := splitProtocols(ni)
		if err != nil {
			return err
		}
		for _, u := range splitUpdates {
			pe := u.GetPath().GetElem()
			if last := pe[len(pe)-1]; last.GetKey()["name"] != "STATIC" {
				// Replace protocols besides static routes, which we ignore to preserve connectivity.
				ups.replace = append(ups.replace, u)
			}
		}
		// Now remove protocols from original update.
		delete(ni, "openconfig-network-instance:protocols")
		delete(ni, "protocols")
	}

	return filterPolicyForwardingInterfaces(ni)
}

func splitProtocols(ni jsonObj) ([]*gnmipb.Update, error) {
	protocols, err := ni.objectField("openconfig-network-instance:protocols")
	if err != nil {
		return nil, err
	}
	if protocols == nil {
		protocols, err = ni.objectField("protocols")
		if protocols == nil || err != nil {
			return nil, err
		}
	}
	protocolList, err := protocols.objectListField("protocol")
	if err != nil {
		return nil, err
	}

	// Make new update for each protocol.
	var ret []*gnmipb.Update
	for _, p := range protocolList {
		name, err := p.stringField("name")
		if err != nil {
			return nil, err
		}
		id, err := p.stringField("identifier")
		if err != nil {
			return nil, err
		}
		update := &gnmipb.Update{
			Path: &gnmipb.Path{Elem: []*gnmipb.PathElem{
				{Name: "network-instances"},
				{Name: "network-instance", Key: map[string]string{"name": "default"}},
				{Name: "protocols"},
				{Name: "protocol", Key: map[string]string{"name": name, "identifier": id}},
			}},
		}
		if err := marshalJSONToUpdate(update, p); err != nil {
			return nil, err
		}

		ret = append(ret, update)
	}
	return ret, nil
}

func transformProtocols(ni jsonObj) error {
	protocols, err := ni.objectField("openconfig-network-instance:protocols")
	if err != nil {
		return err
	}
	if protocols == nil {
		protocols, err = ni.objectField("protocols")
		if protocols == nil || err != nil {
			return err
		}
	}
	protocolList, err := protocols.objectListField("protocol")
	if err != nil {
		return err
	}
	for _, p := range protocolList {
		isis, err := p.objectField("isis")
		if err != nil {
			return err
		}
		if err := transformISIS(isis); err != nil {
			return err
		}
	}
	return nil
}

func transformISIS(isis jsonObj) error {
	// Remove the following deprecated path:
	// /network-instances/network-instance[name=*]/protocols/protocol[identifier='ISIS'][name='isis']/isis/levels/level[level-number=*]/config/enabled
	levels, err := isis.objectField("levels")
	if err != nil {
		return err
	}
	levelList, err := levels.objectListField("level")
	if err != nil {
		return err
	}
	for _, l := range levelList {
		cfg, err := l.objectField("config")
		if err != nil {
			return err
		}
		delete(cfg, "enabled")
	}

	// Ensure ISIS interfaces have the same metric for all levels of a given afi-safi pair.
	intfs, err := isis.objectField("interfaces")
	if intfs == nil || err != nil {
		return err
	}
	intfList, err := intfs.objectListField("interface")
	if err != nil {
		return err
	}
	for _, intf := range intfList {
		intfID, err := intf.stringField("interface-id")
		if err != nil {
			return err
		}
		levels, err := intf.objectField("levels")
		if err != nil {
			return err
		}
		if levels == nil {
			continue
		}
		levelList, err := levels.objectListField("level")
		if err != nil {
			return err
		}

		// Record the relationship between levels, afis, safis, and metrics in maps.
		levelByNum := make(map[int]jsonObj)
		cfgByNumByAfiBySafi := make(map[int]map[string]map[string]jsonObj)
		metricByAfiBySafi := make(map[string]map[string]int)
		for _, level := range levelList {
			levelNum, err := level.intField("level-number")
			if err != nil {
				return err
			}
			levelByNum[levelNum] = level
			cfgByNumByAfiBySafi[levelNum] = make(map[string]map[string]jsonObj)

			afiSafi, err := level.objectField("afi-safi")
			if err != nil {
				return err
			}
			afList, err := afiSafi.objectListField("af")
			if err != nil {
				return err
			}

			for _, af := range afList {
				afi, err := af.stringField("afi-name")
				if err != nil {
					return err
				}
				safi, err := af.stringField("safi-name")
				if err != nil {
					return err
				}
				cfg, err := af.objectField("config")
				if err != nil {
					return err
				}
				cfgBySafi, ok := cfgByNumByAfiBySafi[levelNum][afi]
				if !ok {
					cfgBySafi = make(map[string]jsonObj)
					cfgByNumByAfiBySafi[levelNum][afi] = cfgBySafi
				}
				cfgBySafi[safi] = cfg

				metric, err := cfg.intField("metric")
				if err != nil {
					return err
				}
				if metric == 0 {
					continue
				}
				metricBySafi, ok := metricByAfiBySafi[afi]
				if ok {
					prevMetric, ok := metricBySafi[safi]
					if ok && prevMetric != metric {
						return fmt.Errorf("multiple metrics for ISIS interface %q: %d and %d", intfID, prevMetric, metric)
					}
					metricBySafi[safi] = metric
				} else {
					metricByAfiBySafi[afi] = map[string]int{safi: metric}
				}
			}
		}

		// If some levels have metrics, make sure there's an entry for both levels.
		for _, levelNum := range []int{1, 2} {
			cfgByAfiBySafi := cfgByNumByAfiBySafi[levelNum]
			level, ok := levelByNum[levelNum]
			if !ok {
				level = jsonObj{
					"level-number": levelNum,
					"config": jsonObj{
						"level-number": levelNum,
					},
				}
				levels["level"] = append(levelList, level)
			}

			for afi, metricBySafi := range metricByAfiBySafi {
				for safi, metric := range metricBySafi {
					cfg, ok := cfgByAfiBySafi[afi][safi]
					if ok {
						cfg["metric"] = metric
						continue
					}
					afiSafi, err := level.objectField("afi-safi")
					if err != nil {
						return err
					}
					if afiSafi == nil {
						afiSafi = jsonObj{}
						level["afi-safi"] = afiSafi
					}
					af, err := afiSafi.objectListField("af")
					if err != nil {
						return err
					}
					afiSafi["af"] = append(af, jsonObj{
						"afi-name":  afi,
						"safi-name": safi,
						"config": jsonObj{
							"afi-name":  afi,
							"safi-name": safi,
							"metric":    metric,
						},
					})
				}
			}
		}
	}

	return nil
}

// filterPolicyForwardingInterfaces removes unwanted interfaces from policy forwarding. This is done
// to ensure that interfaces that must remain untouched do not have their behavior modified.
// It modifies the update in-place and leaves it unmodified if there are no unwanted paths.
func filterPolicyForwardingInterfaces(ni jsonObj) error {
	pf, err := ni.objectField("openconfig-network-instance:policy-forwarding")
	if pf == nil || err != nil {
		return err
	}
	intfs, err := pf.objectField("interfaces")
	if intfs == nil || err != nil {
		return err
	}
	intfList, err := intfs.objectListField("interface")
	if err != nil {
		return err
	}

	var newIntfList []jsonObj
	for _, intf := range intfList {
		id, err := intf.stringField("interface-id")
		if err != nil {
			return err
		}
		if !isProtectedBundle(id) {
			newIntfList = append(newIntfList, intf)
		}
	}
	intfs["interface"] = newIntfList
	return nil
}

// filterLACP removes the LACP config for all bundles other than the bundles whose members have been
// remapped by SetInterfaceMap. It modifies the update in place and leaves it unmodified if there
// are no unwanted LACP interfaces.
func filterLACP(update *gnmipb.Update) error {
	lacp, err := unmarshalJSONFromUpdate(update)
	if err != nil {
		return err
	}
	intfs, err := lacp.objectField("openconfig-lacp:interfaces")
	if intfs == nil || err != nil {
		return err
	}
	intfList, err := intfs.objectListField("interface")
	if err != nil {
		return err
	}
	var newIntfList []jsonObj
	for _, intf := range intfList {
		name, err := intf.stringField("name")
		if err != nil {
			return err
		}
		if !isProtectedBundle(name) {
			newIntfList = append(newIntfList, intf)
		}
	}
	intfs["interface"] = newIntfList
	return marshalJSONToUpdate(update, lacp)
}

// remapInterfaces transforms the interfaces path by renaming interfaces according to the provided
// interface mapping. It returns a slice of updates, with one update per interface. This is done so
// each individual interface can be replaced without replacing the whole /interfaces subtree, which
// would overwrite existing interfaces and potentially affect device connectivity.
func remapInterfaces(update *gnmipb.Update, intfMap map[string]string) ([]*gnmipb.Update, error) {
	intfs, err := unmarshalJSONFromUpdate(update)
	if err != nil {
		return nil, err
	}
	intfList, err := intfs.objectListField("openconfig-interfaces:interface")
	if err != nil {
		return nil, err
	}
	if intfList == nil {
		intfList, err = intfs.objectListField("interface")
		if err != nil {
			return nil, err
		}
	}

	var updates []*gnmipb.Update
	for _, intf := range intfList {
		name, err := intf.stringField("name")
		if err != nil {
			return nil, err
		}

		newName, ok := intfMap[name]
		if ok {
			intf["name"] = newName
			cfg, err := intf.objectField("config")
			if err != nil {
				return nil, err
			}
			cfg["name"] = newName
			u, err := interfaceUpdate(newName, intf)
			if err != nil {
				return nil, err
			}
			if u != nil {
				updates = append(updates, u)
			}
		} else if strings.HasPrefix(name, "Port-Channel") && !isProtectedBundle(name) {
			// TODO(nhawke): Support other vendor bundle names.
			u, err := interfaceUpdate(name, intf)
			if err != nil {
				return nil, err
			}
			if u != nil {
				updates = append(updates, u)
			}
		}
	}
	return updates, nil
}

func interfaceUpdate(name string, val jsonObj) (*gnmipb.Update, error) {
	update := &gnmipb.Update{
		Path: &gnmipb.Path{
			Elem: []*gnmipb.PathElem{{
				Name: "interfaces",
			}, {
				Name: "interface",
				Key:  map[string]string{"name": name},
			}},
		},
	}
	err := marshalJSONToUpdate(update, val)
	return update, err
}

// isProtectedBundle returns whether a given bundle is protected. Protected bundles should not have
// their config modified or overridden. For running on Aristas in the functional testbeds,
// Port-Channel1 and Port-Channel2 are protected to maintain connectivity to the device.
// TODO(nhawke): Support protected bundles for other vendors.
func isProtectedBundle(name string) bool {
	return name == "Port-Channel1" || name == "Port-Channel2"
}

func unmarshalJSONFromUpdate(update *gnmipb.Update) (jsonObj, error) {
	var jo jsonObj
	if err := json.Unmarshal(update.GetVal().GetJsonIetfVal(), &jo); err != nil {
		return nil, fmt.Errorf("unmarshalling %v: %w", update.GetVal().GetJsonIetfVal(), err)
	}
	return jo, nil
}

func marshalJSONToUpdate(update *gnmipb.Update, jo jsonObj) error {
	val, err := json.Marshal(jo)
	if err != nil {
		return fmt.Errorf("marshalling %v: %w", jo, err)
	}
	update.Val = &gnmipb.TypedValue{Value: &gnmipb.TypedValue_JsonIetfVal{val}}
	return nil
}
