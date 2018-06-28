// Copyright © 2018 Heptio
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

// Package contour contains the translation business logic that listens
// to Kubernetes ResourceEventHandler events and translates those into
// additions/deletions in caches connected to the Envoy xDS gRPC API server.
package contour

import "github.com/heptio/contour/internal/dag"

// DAGAdapter wraps a dag.ResourceEventHandler to hook post update cache
// generation.
type DAGAdapter struct {
	dag.ResourceEventHandler // provides a Visit method
}

func (d *DAGAdapter) OnAdd(obj interface{}) {
	d.ResourceEventHandler.OnAdd(obj)
}

func (d *DAGAdapter) OnUpdate(oldObj, newObj interface{}) {
	d.ResourceEventHandler.OnUpdate(oldObj, newObj)
}

func (d *DAGAdapter) OnDelete(obj interface{}) {
	d.ResourceEventHandler.OnDelete(obj)
}
