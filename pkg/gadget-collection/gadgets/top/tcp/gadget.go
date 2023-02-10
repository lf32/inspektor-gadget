// Copyright 2019-2021 The Inspektor Gadget authors
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

package tcptop

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	gadgetv1alpha1 "github.com/inspektor-gadget/inspektor-gadget/pkg/apis/gadget/v1alpha1"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/columns/sort"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/gadget-collection/gadgets"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/gadgets/top"
	tcptoptracer "github.com/inspektor-gadget/inspektor-gadget/pkg/gadgets/top/tcp/tracer"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/gadgets/top/tcp/types"
)

type Trace struct {
	helpers gadgets.GadgetHelpers

	started bool
	tracer  *tcptoptracer.Tracer
}

type TraceFactory struct {
	gadgets.BaseFactory
}

func NewFactory() gadgets.TraceFactory {
	return &TraceFactory{
		BaseFactory: gadgets.BaseFactory{DeleteTrace: deleteTrace},
	}
}

func (f *TraceFactory) Description() string {
	cols := types.GetColumns()
	validCols, _ := sort.FilterSortableColumns(cols.ColumnMap, cols.GetColumnNames())

	t := `tcptop shows command generating TCP connections, with container details.

The following parameters are supported:
- %s: Output interval, in seconds. (default %d)
- %s: Maximum rows to print. (default %d)
- %s: The field to sort the results by (%s). (default %s)
- %s: Only get events for this PID (default to all).
- %s: Only get events for this IP version. (either 4 or 6, default to all)`
	return fmt.Sprintf(t, top.IntervalParam, top.IntervalDefault,
		top.MaxRowsParam, top.MaxRowsDefault,
		top.SortByParam, strings.Join(validCols, ","), strings.Join(types.SortByDefault, ","),
		types.PidParam, types.FamilyParam)
}

func (f *TraceFactory) OutputModesSupported() map[gadgetv1alpha1.TraceOutputMode]struct{} {
	return map[gadgetv1alpha1.TraceOutputMode]struct{}{
		gadgetv1alpha1.TraceOutputModeStream: {},
	}
}

func deleteTrace(name string, t interface{}) {
	trace := t.(*Trace)
	if trace.tracer != nil {
		trace.tracer.Stop()
	}
}

func (f *TraceFactory) Operations() map[gadgetv1alpha1.Operation]gadgets.TraceOperation {
	n := func() interface{} {
		return &Trace{
			helpers: f.Helpers,
		}
	}

	return map[gadgetv1alpha1.Operation]gadgets.TraceOperation{
		gadgetv1alpha1.OperationStart: {
			Doc: "Start tcptop gadget",
			Operation: func(name string, trace *gadgetv1alpha1.Trace) {
				f.LookupOrCreate(name, n).(*Trace).Start(trace)
			},
		},
		gadgetv1alpha1.OperationStop: {
			Doc: "Stop tcptop gadget",
			Operation: func(name string, trace *gadgetv1alpha1.Trace) {
				f.LookupOrCreate(name, n).(*Trace).Stop(trace)
			},
		},
	}
}

func (t *Trace) Start(trace *gadgetv1alpha1.Trace) {
	if t.started {
		trace.Status.State = gadgetv1alpha1.TraceStateStarted
		return
	}

	traceName := gadgets.TraceName(trace.ObjectMeta.Namespace, trace.ObjectMeta.Name)

	maxRows := top.MaxRowsDefault
	intervalSeconds := top.IntervalDefault
	sortBy := types.SortByDefault
	targetPid := int32(0)
	targetFamily := int32(-1)

	if trace.Spec.Parameters != nil {
		params := trace.Spec.Parameters
		var err error

		if val, ok := params[top.MaxRowsParam]; ok {
			maxRows, err = strconv.Atoi(val)
			if err != nil {
				trace.Status.OperationError = fmt.Sprintf("%q is not valid for %q", val, top.MaxRowsParam)
				return
			}
		}

		if val, ok := params[top.IntervalParam]; ok {
			intervalSeconds, err = strconv.Atoi(val)
			if err != nil {
				trace.Status.OperationError = fmt.Sprintf("%q is not valid for %q", val, top.IntervalParam)
				return
			}
		}

		if val, ok := params[top.SortByParam]; ok {
			sortByColumns := strings.Split(val, ",")

			_, invalidCols := sort.FilterSortableColumns(types.GetColumns().ColumnMap, sortByColumns)
			if len(invalidCols) > 0 {
				trace.Status.OperationError = fmt.Sprintf("%q are not valid for %q", strings.Join(invalidCols, ","), top.SortByParam)
				return
			}

			sortBy = sortByColumns
		}

		if val, ok := params[types.PidParam]; ok {
			pid, err := strconv.ParseInt(val, 10, 32)
			if err != nil {
				trace.Status.OperationError = fmt.Sprintf("%q is not valid for %q", val, types.PidParam)
				return
			}

			targetPid = int32(pid)
		}

		if val, ok := params[types.FamilyParam]; ok {
			targetFamily, err = types.ParseFilterByFamily(val)
			if err != nil {
				trace.Status.OperationError = fmt.Sprintf("%q is not valid for %q", val, types.FamilyParam)
				return
			}
		}
	}

	mountNsMap, err := t.helpers.TracerMountNsMap(traceName)
	if err != nil {
		trace.Status.OperationError = fmt.Sprintf("failed to find tracer's mount ns map: %s", err)
		return
	}
	config := &tcptoptracer.Config{
		MaxRows:      maxRows,
		Interval:     time.Second * time.Duration(intervalSeconds),
		SortBy:       sortBy,
		MountnsMap:   mountNsMap,
		TargetPid:    targetPid,
		TargetFamily: targetFamily,
	}

	eventCallback := func(ev *top.Event[types.Stats]) {
		r, err := json.Marshal(ev)
		if err != nil {
			log.Warnf("Gadget %s: Failed to marshall event: %s", trace.Spec.Gadget, err)
			return
		}
		t.helpers.PublishEvent(traceName, string(r))
	}

	tracer, err := tcptoptracer.NewTracer(config, t.helpers, eventCallback)
	if err != nil {
		trace.Status.OperationError = fmt.Sprintf("failed to create tracer: %s", err)
		return
	}

	t.tracer = tracer
	t.started = true

	trace.Status.State = gadgetv1alpha1.TraceStateStarted
}

func (t *Trace) Stop(trace *gadgetv1alpha1.Trace) {
	if !t.started {
		trace.Status.OperationError = "Not started"
		return
	}

	t.tracer.Stop()
	t.tracer = nil
	t.started = false

	trace.Status.State = gadgetv1alpha1.TraceStateStopped
}
