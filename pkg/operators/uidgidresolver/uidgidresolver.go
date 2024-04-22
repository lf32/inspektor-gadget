// Copyright 2024 The Inspektor Gadget authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package uidgidresolver provides an operator that enriches events by looking
// up uid and gid resolving them to the corresponding username and groupname.
// Only /etc/passwd and /etc/group is read on the host. Therefore the name for a
// corresponding id could be wrong.
package uidgidresolver

import (
	"github.com/inspektor-gadget/inspektor-gadget/pkg/gadgets"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/operators"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/params"
)

const (
	OperatorName = "UidGidResolver"
)

type UidResolverInterface interface {
	GetUid() uint32
	SetUserName(string)
}

type GidResolverInterface interface {
	GetGid() uint32
	SetGroupName(string)
}

type UidGidResolver struct{}

func (k *UidGidResolver) Name() string {
	return OperatorName
}

func (k *UidGidResolver) Description() string {
	return "UidGidResolver resolves uid and gid to username and groupname"
}

func (k *UidGidResolver) GlobalParamDescs() params.ParamDescs {
	return nil
}

func (k *UidGidResolver) ParamDescs() params.ParamDescs {
	return nil
}

func (k *UidGidResolver) Dependencies() []string {
	return nil
}

func (k *UidGidResolver) CanOperateOn(gadget gadgets.GadgetDesc) bool {
	_, hasUidResolverInterface := gadget.EventPrototype().(UidResolverInterface)
	_, hasGidResolverInterface := gadget.EventPrototype().(GidResolverInterface)
	return hasUidResolverInterface || hasGidResolverInterface
}

func (k *UidGidResolver) Init(params *params.Params) error {
	return nil
}

func (k *UidGidResolver) Close() error {
	return nil
}

func (k *UidGidResolver) Instantiate(gadgetCtx operators.GadgetContext, gadgetInstance any, params *params.Params) (operators.OperatorInstance, error) {
	uidGidCache := GetUserGroupCache()

	return &UidGidResolverInstance{
		gadgetCtx:      gadgetCtx,
		gadgetInstance: gadgetInstance,
		uidGidCache:    uidGidCache,
	}, nil
}

type UidGidResolverInstance struct {
	gadgetCtx      operators.GadgetContext
	gadgetInstance any
	uidGidCache    UserGroupCache
}

func (m *UidGidResolverInstance) Name() string {
	return "UidGidResolverInstance"
}

func (m *UidGidResolverInstance) PreGadgetRun() error {
	return m.uidGidCache.Start()
}

func (m *UidGidResolverInstance) PostGadgetRun() error {
	m.uidGidCache.Stop()
	return nil
}

func (m *UidGidResolverInstance) enrich(ev any) {
	uidResolver := ev.(UidResolverInterface)
	if uidResolver != nil {
		uid := uidResolver.GetUid()
		uidResolver.SetUserName(m.uidGidCache.GetUsername(uid))
	}

	gidResolver := ev.(GidResolverInterface)
	if gidResolver != nil {
		gid := gidResolver.GetGid()
		gidResolver.SetGroupName(m.uidGidCache.GetGroupname(gid))
	}
}

func (m *UidGidResolverInstance) EnrichEvent(ev any) error {
	m.enrich(ev)
	return nil
}

func init() {
	operators.Register(&UidGidResolver{})
}
