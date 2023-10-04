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
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	gepb "github.com/openconfig/gnmi/proto/gnmi_ext"
)

func TestTransformNetworkInstance(t *testing.T) {
	tests := []struct {
		desc   string
		update *gnmipb.Update
		want   *updates
	}{{
		desc: "mgmtVrf",
		update: &gnmipb.Update{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "network-instances"},
					{Name: "network-instance", Key: map[string]string{"name": "mgmtVrf"}},
				},
			},
			Val: jsonVal(t, jsonObj{
				"name": "someName",
				"foo":  "bar",
				"config": jsonObj{
					"name": "someName",
					"type": "someType",
				},
			}),
		},
		want: &updates{
			update: []*gnmipb.Update{{
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "mgmtVrf"}},
					},
				},
				Val: jsonVal(t, jsonObj{
					"name": "mgmtVrf",
					"config": jsonObj{
						"name": "mgmtVrf",
						"type": "L3VRF",
					},
				}),
			}},
		},
	}, {
		desc: "isis config enabled",
		update: &gnmipb.Update{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "network-instances"},
					{Name: "network-instance", Key: map[string]string{"name": "myNI"}},
				},
			},
			Val: jsonVal(t, jsonObj{
				"openconfig-network-instance:protocols": jsonObj{
					"protocol": []jsonObj{{
						"isis": jsonObj{
							"levels": jsonObj{
								"level": []jsonObj{{
									"config": jsonObj{
										"enabled": true,
									},
								}},
							},
						},
					}},
				},
			}),
		},
		want: &updates{
			update: []*gnmipb.Update{{
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "myNI"}},
					},
				},
				Val: jsonVal(t, jsonObj{
					"openconfig-network-instance:protocols": jsonObj{
						"protocol": []jsonObj{{
							"isis": jsonObj{
								"levels": jsonObj{
									"level": []jsonObj{{
										"config": jsonObj{},
									}},
								},
							},
						}},
					},
				}),
			}},
		},
	}, {
		desc: "isis same metric",
		update: &gnmipb.Update{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "network-instances"},
					{Name: "network-instance", Key: map[string]string{"name": "myNI"}},
				},
			},
			Val: jsonVal(t, jsonObj{
				"openconfig-network-instance:protocols": jsonObj{
					"protocol": []jsonObj{{
						"isis": jsonObj{
							"interfaces": jsonObj{
								"interface": []jsonObj{{
									"interface-id": "intf1",
								}, {
									"interface-id": "intf2",
									"levels": jsonObj{
										"level": []jsonObj{{
											"level-number": 1,
											"config": jsonObj{
												"level-number": 1,
											},
											"afi-safi": jsonObj{
												"af": []jsonObj{{
													"afi-name":  "afi2",
													"safi-name": "safi2",
													"config": jsonObj{
														"afi-name":  "afi2",
														"safi-name": "safi2",
														"metric":    10,
													},
												}},
											},
										}},
									},
								}, {
									"interface-id": "intf3",
									"levels": jsonObj{
										"level": []jsonObj{{
											"level-number": 1,
											"config": jsonObj{
												"level-number": 1,
											},
											"other-property": "foo",
										}, {
											"level-number": 2,
											"config": jsonObj{
												"level-number": 2,
											},
											"afi-safi": jsonObj{
												"af": []jsonObj{{
													"afi-name":  "afi3",
													"safi-name": "safi3",
													"config": jsonObj{
														"afi-name":  "afi3",
														"safi-name": "safi3",
														"metric":    15,
													},
												}},
											},
										}},
									},
								}, {
									"interface-id": "intf4",
									"levels": jsonObj{
										"level": []jsonObj{{
											"level-number": 1,
											"config": jsonObj{
												"level-number": 1,
											},
											"afi-safi": jsonObj{
												"af": []jsonObj{{
													"afi-name":  "afi4",
													"safi-name": "safi4",
													"config": jsonObj{
														"afi-name":  "afi4",
														"safi-name": "safi4",
														"metric":    20,
													},
												}},
											},
										}, {
											"level-number": 2,
											"config": jsonObj{
												"level-number": 2,
											},
											"afi-safi": jsonObj{
												"af": []jsonObj{{
													"afi-name":  "afi4",
													"safi-name": "safi4",
													"config": jsonObj{
														"afi-name":  "afi4",
														"safi-name": "safi4",
														"metric":    20,
													},
												}},
											},
										}},
									},
								}},
							},
						},
					}},
				},
			}),
		},
		want: &updates{
			update: []*gnmipb.Update{{
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "myNI"}},
					},
				},
				Val: jsonVal(t, jsonObj{
					"openconfig-network-instance:protocols": jsonObj{
						"protocol": []jsonObj{{
							"isis": jsonObj{
								"interfaces": jsonObj{
									"interface": []jsonObj{{
										"interface-id": "intf1",
									}, {
										"interface-id": "intf2",
										"levels": jsonObj{
											"level": []jsonObj{{
												"level-number": 1,
												"config": jsonObj{
													"level-number": 1,
												},
												"afi-safi": jsonObj{
													"af": []jsonObj{{
														"afi-name":  "afi2",
														"safi-name": "safi2",
														"config": jsonObj{
															"afi-name":  "afi2",
															"safi-name": "safi2",
															"metric":    10,
														},
													}},
												},
											}, {
												"level-number": 2,
												"config": jsonObj{
													"level-number": 2,
												},
												"afi-safi": jsonObj{
													"af": []jsonObj{{
														"afi-name":  "afi2",
														"safi-name": "safi2",
														"config": jsonObj{
															"afi-name":  "afi2",
															"safi-name": "safi2",
															"metric":    10,
														},
													}},
												},
											}},
										},
									}, {
										"interface-id": "intf3",
										"levels": jsonObj{
											"level": []jsonObj{{
												"level-number": 1,
												"config": jsonObj{
													"level-number": 1,
												},
												"other-property": "foo",
												"afi-safi": jsonObj{
													"af": []jsonObj{{
														"afi-name":  "afi3",
														"safi-name": "safi3",
														"config": jsonObj{
															"afi-name":  "afi3",
															"safi-name": "safi3",
															"metric":    15,
														},
													}},
												},
											}, {
												"level-number": 2,
												"config": jsonObj{
													"level-number": 2,
												},
												"afi-safi": jsonObj{
													"af": []jsonObj{{
														"afi-name":  "afi3",
														"safi-name": "safi3",
														"config": jsonObj{
															"afi-name":  "afi3",
															"safi-name": "safi3",
															"metric":    15,
														},
													}},
												},
											}},
										},
									}, {
										"interface-id": "intf4",
										"levels": jsonObj{
											"level": []jsonObj{{
												"level-number": 1,
												"config": jsonObj{
													"level-number": 1,
												},
												"afi-safi": jsonObj{
													"af": []jsonObj{{
														"afi-name":  "afi4",
														"safi-name": "safi4",
														"config": jsonObj{
															"afi-name":  "afi4",
															"safi-name": "safi4",
															"metric":    20,
														},
													}},
												},
											}, {
												"level-number": 2,
												"config": jsonObj{
													"level-number": 2,
												},
												"afi-safi": jsonObj{
													"af": []jsonObj{{
														"afi-name":  "afi4",
														"safi-name": "safi4",
														"config": jsonObj{
															"afi-name":  "afi4",
															"safi-name": "safi4",
															"metric":    20,
														},
													}},
												},
											}},
										},
									}},
								},
							},
						}},
					},
				}),
			}},
		},
	}, {
		desc: "split protocols",
		update: &gnmipb.Update{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "network-instances"},
					{Name: "network-instance", Key: map[string]string{"name": "default"}},
				},
			},
			Val: jsonVal(t,
				jsonObj{
					"something": "another thing",
					"openconfig-network-instance:protocols": jsonObj{
						"protocol": []jsonObj{{
							"name":       "foo",
							"identifier": "bar",
							"config": jsonObj{
								"baba": "yes",
							},
						}, {
							"name":       "spam",
							"identifier": "eggs",
							"config": jsonObj{
								"baba": "no",
							},
						}},
					},
				},
			),
		},
		want: &updates{
			update: []*gnmipb.Update{{
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "default"}},
					},
				},
				Val: jsonVal(t,
					jsonObj{
						"something": "another thing",
					},
				),
			}},
			replace: []*gnmipb.Update{{
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "default"}},
						{Name: "protocols"},
						{Name: "protocol", Key: map[string]string{"name": "foo", "identifier": "bar"}},
					},
				},
				Val: jsonVal(t,
					jsonObj{
						"name":       "foo",
						"identifier": "bar",
						"config": jsonObj{
							"baba": "yes",
						},
					},
				),
			}, {
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "default"}},
						{Name: "protocols"},
						{Name: "protocol", Key: map[string]string{"name": "spam", "identifier": "eggs"}},
					},
				},
				Val: jsonVal(t,
					jsonObj{
						"name":       "spam",
						"identifier": "eggs",
						"config": jsonObj{
							"baba": "no",
						},
					},
				),
			}},
		},
	}, {
		desc: "isis transform with split",
		update: &gnmipb.Update{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "network-instances"},
					{Name: "network-instance", Key: map[string]string{"name": "default"}},
				},
			},
			Val: jsonVal(t, jsonObj{
				"openconfig-network-instance:protocols": jsonObj{
					"protocol": []jsonObj{{
						"identifier": "isisID",
						"name":       "isisName",
						"isis": jsonObj{
							"levels": jsonObj{
								"level": []jsonObj{{
									"config": jsonObj{
										"enabled": true,
									},
								}},
							},
						},
					}},
				},
			}),
		},
		want: &updates{
			update: []*gnmipb.Update{{
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "default"}},
					},
				},
				Val: jsonVal(t, jsonObj{}),
			}},
			replace: []*gnmipb.Update{{
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "default"}},
						{Name: "protocols"},
						{Name: "protocol", Key: map[string]string{"identifier": "isisID", "name": "isisName"}},
					},
				},
				Val: jsonVal(t, jsonObj{
					"identifier": "isisID",
					"name":       "isisName",
					"isis": jsonObj{
						"levels": jsonObj{
							"level": []jsonObj{{
								"config": jsonObj{},
							}},
						},
					},
				}),
			}},
		},
	}, {
		desc: "policy forwarding",
		update: &gnmipb.Update{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "network-instances"},
					{Name: "network-instance", Key: map[string]string{"name": "myNI"}},
				},
			},
			Val: jsonVal(t, jsonObj{
				"openconfig-network-instance:policy-forwarding": jsonObj{
					"interfaces": jsonObj{
						"interface": []jsonObj{{
							"interface-id": "Port-Channel1",
							"some key":     "some value",
						}, {
							"interface-id": "Port-Channel2",
							"some key":     "some value2",
						}, {
							"interface-id": "some other interface",
							"some key":     "some value3",
						}},
					},
				},
			}),
		},
		want: &updates{
			update: []*gnmipb.Update{{
				Path: &gnmipb.Path{
					Elem: []*gnmipb.PathElem{
						{Name: "network-instances"},
						{Name: "network-instance", Key: map[string]string{"name": "myNI"}},
					},
				},
				Val: jsonVal(t, jsonObj{
					"openconfig-network-instance:policy-forwarding": jsonObj{
						"interfaces": jsonObj{
							"interface": []jsonObj{{
								"interface-id": "some other interface",
								"some key":     "some value3",
							}},
						},
					},
				}),
			}},
		},
	}}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			got, err := transformNetworkInstance(test.update)
			if err != nil {
				t.Errorf("transformNetworkInstance() got unexpected error: %v", err)
			}
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmp.AllowUnexported(updates{})); diff != "" {
				t.Errorf("transformNetworkInstance() got unexpected result (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFilterLACP(t *testing.T) {
	in := &gnmipb.Update{
		Path: &gnmipb.Path{
			Elem: []*gnmipb.PathElem{
				{Name: "lacp"},
			},
		},
		Val: jsonVal(t, jsonObj{
			"openconfig-lacp:interfaces": jsonObj{
				"interface": []jsonObj{
					{
						"name":     "Port-Channel1",
						"some key": "some value",
					},
					{
						"name":     "Port-Channel2",
						"some key": "some value2",
					},
					{
						"name":     "some other interface",
						"some key": "some value3",
					},
				},
			},
		}),
	}
	want := &gnmipb.Update{
		Path: &gnmipb.Path{
			Elem: []*gnmipb.PathElem{
				{Name: "lacp"},
			},
		},
		Val: jsonVal(t, jsonObj{
			"openconfig-lacp:interfaces": jsonObj{
				"interface": []jsonObj{
					{
						"name":     "some other interface",
						"some key": "some value3",
					},
				},
			},
		}),
	}

	if err := filterLACP(in); err != nil {
		t.Errorf("filterLACP() got unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, in, protocmp.Transform()); diff != "" {
		t.Errorf("filterLACP() got unexpected result (-want +got):\n%s", diff)
	}
}

func TestRemapInterfaces(t *testing.T) {
	in := &gnmipb.Update{
		Path: &gnmipb.Path{
			Elem: []*gnmipb.PathElem{
				{Name: "interfaces"},
			},
		},
		Val: jsonVal(t, jsonObj{
			"interface": []jsonObj{
				{
					"name": "foo",
					"config": jsonObj{
						"name": "foo",
					},
					"key": "value",
				},
				{
					"name": "bar",
					"config": jsonObj{
						"name": "bar",
					},
					"key": "value2",
				},
				{
					"name": "Port-Channel1",
					"config": jsonObj{
						"name": "Port-Channel1",
					},
					"key": "value3",
				},
				{
					"name": "Port-Channel3",
					"config": jsonObj{
						"name": "Port-Channel3",
					},
					"key": "value4",
				},
			},
		}),
	}

	intfMap := map[string]string{
		"foo": "spam",
		"bar": "eggs",
	}

	want := []*gnmipb.Update{
		{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "interfaces"},
					{Name: "interface", Key: map[string]string{"name": "spam"}},
				},
			},
			Val: jsonVal(t, jsonObj{
				"name": "spam",
				"config": jsonObj{
					"name": "spam",
				},
				"key": "value",
			}),
		},
		{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "interfaces"},
					{Name: "interface", Key: map[string]string{"name": "eggs"}},
				},
			},
			Val: jsonVal(t, jsonObj{
				"name": "eggs",
				"config": jsonObj{
					"name": "eggs",
				},
				"key": "value2",
			}),
		},
		{
			Path: &gnmipb.Path{
				Elem: []*gnmipb.PathElem{
					{Name: "interfaces"},
					{Name: "interface", Key: map[string]string{"name": "Port-Channel3"}},
				},
			},
			Val: jsonVal(t, jsonObj{
				"name": "Port-Channel3",
				"config": jsonObj{
					"name": "Port-Channel3",
				},
				"key": "value4",
			}),
		},
	}

	out, err := remapInterfaces(in, intfMap)
	if err != nil {
		t.Errorf("remapInterfaces() got unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, out, protocmp.Transform()); diff != "" {
		t.Errorf("remapInterfaces() got unexpected result (-want +got):\n%s", diff)
	}
}

func TestTransformExtensions(t *testing.T) {
	req := &gnmipb.SetRequest{
		Extension: []*gepb.Extension{{
			Ext: &gepb.Extension_MasterArbitration{&gepb.MasterArbitration{}},
		}, {
			Ext: &gepb.Extension_RegisteredExt{&gepb.RegisteredExtension{}},
		}},
	}
	want := &gnmipb.SetRequest{
		Extension: []*gepb.Extension{{
			Ext: &gepb.Extension_RegisteredExt{&gepb.RegisteredExtension{}},
		}},
	}
	got, err := transformSet(req, nil)
	if err != nil {
		t.Errorf("transformSet() got unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("transformSet() got unexpected result (-want +got):\n%s", diff)
	}
}

func jsonVal(t *testing.T, j jsonObj) *gnmipb.TypedValue {
	t.Helper()
	jb, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
		return nil
	}
	return &gnmipb.TypedValue{Value: &gnmipb.TypedValue_JsonIetfVal{jb}}
}
