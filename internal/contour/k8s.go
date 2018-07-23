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

import (
	"github.com/heptio/contour/internal/dag"
	"k8s.io/api/extensions/v1beta1"
)

const DEFAULT_INGRESS_CLASS = "contour"

// ResourceEventHandler implements cache.ResourceEventHandler, filters
// k8s watcher events towards a dag.Builder (which also implements the
// same interface) and calls through to the CacheHandler to notify it
// that the contents of the dag.Builder have changed.
type ResourceEventHandler struct {
	// Contour's IngressClass.
	// If not set, defaults to DEFAULT_INGRESS_CLASS.
	IngressClass string

	dag.Builder

	CacheHandler
}

func (reh *ResourceEventHandler) OnAdd(obj interface{}) {
	if !reh.validIngressClass(obj) {
		return
	}
	reh.Insert(obj)
	reh.update()
}

func (reh *ResourceEventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldValid, newValid := reh.validIngressClass(oldObj), reh.validIngressClass(newObj)
	switch {
	case !oldValid && !newValid:
		// the old object did not match the ingress class, nor does
		// the new object, nothing to do
	case oldValid && !newValid:
		// if the old object was valid, and the replacement is not, then we need
		// to remove the old object and _not_ insert the new object.
		reh.OnDelete(oldObj)
	default:
		reh.Remove(oldObj)
		reh.Insert(newObj)
		reh.update()
	}
}

func (reh *ResourceEventHandler) OnDelete(obj interface{}) {
	// no need to check ingress class here
	reh.Remove(obj)
	reh.update()
}

func (reh *ResourceEventHandler) update() {
	reh.CacheHandler.update(&reh.Builder)
}

// validIngressClass returns true iff:
//
// 1. obj is not of type *v1beta1.Ingress.
// 2. obj has no ingress.class annotation.
// 2. obj's ingress.class annotation matches d.IngressClass.
func (reh *ResourceEventHandler) validIngressClass(obj interface{}) bool {
	i, ok := obj.(*v1beta1.Ingress)
	if !ok {
		return true
	}
	class, ok := i.Annotations["kubernetes.io/ingress.class"]
	return !ok || class == reh.ingressClass()
}

// ingressClass returns the IngressClass
// or DEFAULT_INGRESS_CLASS if not configured.
func (reh *ResourceEventHandler) ingressClass() string {
	if reh.IngressClass != "" {
		return reh.IngressClass
	}
	return DEFAULT_INGRESS_CLASS
}
