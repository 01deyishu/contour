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
	"github.com/heptio/contour/internal/k8s"
	"github.com/heptio/contour/internal/metrics"
	"github.com/sirupsen/logrus"
	"k8s.io/api/extensions/v1beta1"
)

const DEFAULT_INGRESS_CLASS = "contour"

// DAGAdapter wraps a dag.DAG to hook post update cache generation.
type DAGAdapter struct {
	// Contour's IngressClass.
	// If not set, defaults to DEFAULT_INGRESS_CLASS.
	IngressClass string

	dag.DAG
	ListenerCache
	RouteCache
	ClusterCache

	IngressRouteStatus *k8s.IngressRouteStatus
	logrus.FieldLogger
	metrics.Metrics
}

func (d *DAGAdapter) OnAdd(obj interface{}) {
	if !d.validIngressClass(obj) {
		return
	}
	d.Insert(obj)
	d.update()
}

func (d *DAGAdapter) OnUpdate(oldObj, newObj interface{}) {
	oldValid, newValid := d.validIngressClass(oldObj), d.validIngressClass(newObj)
	switch {
	case !oldValid && !newValid:
		// the old object did not match the ingress class, nor does
		// the new object, nothing to do
	case oldValid && !newValid:
		// if the old object was valid, and the replacement is not, then we need
		// to remove the old object and _not_ insert the new object.
		d.OnDelete(oldObj)
	default:
		d.Remove(oldObj)
		d.Insert(newObj)
		d.update()
	}
}

func (d *DAGAdapter) OnDelete(obj interface{}) {
	// no need to check ingress class here
	d.Remove(obj)
	d.update()
}

func (d *DAGAdapter) update() {
	d.Recompute()
	d.setIngressRouteStatus()
	d.updateListeners()
	d.updateRoutes()
	d.updateClusters()
	d.updateIngressRouteMetric()
}

func (d *DAGAdapter) setIngressRouteStatus() {
	for _, s := range d.Statuses() {
		err := d.IngressRouteStatus.SetStatus(s.Status, s.Description, s.Object)
		if err != nil {
			d.FieldLogger.Errorf("Error Setting Status of IngressRoute: ", err)
		}
	}
}

// validIngressClass returns true iff:
//
// 1. obj is not of type *v1beta1.Ingress.
// 2. obj has no ingress.class annotation.
// 2. obj's ingress.class annotation matches d.IngressClass.
func (d *DAGAdapter) validIngressClass(obj interface{}) bool {
	i, ok := obj.(*v1beta1.Ingress)
	if !ok {
		return true
	}
	class, ok := i.Annotations["kubernetes.io/ingress.class"]
	return !ok || class == d.ingressClass()
}

// ingressClass returns the IngressClass
// or DEFAULT_INGRESS_CLASS if not configured.
func (d *DAGAdapter) ingressClass() string {
	if d.IngressClass != "" {
		return d.IngressClass
	}
	return DEFAULT_INGRESS_CLASS
}

func (d *DAGAdapter) updateListeners() {
	v := listenerVisitor{
		ListenerCache: &d.ListenerCache,
		DAG:           &d.DAG,
	}
	d.ListenerCache.Update(v.Visit())
}

func (d *DAGAdapter) updateRoutes() {
	v := routeVisitor{
		RouteCache: &d.RouteCache,
		DAG:        &d.DAG,
	}
	routes := v.Visit()
	d.RouteCache.Update(routes)
}

func (d *DAGAdapter) updateClusters() {
	v := clusterVisitor{
		ClusterCache: &d.ClusterCache,
		DAG:          &d.DAG,
	}
	d.clusterCache.Update(v.Visit())
}

func (d *DAGAdapter) updateIngressRouteMetric() {
	metrics := d.calculateIngressRouteMetric()
	d.Metrics.SetIngressRouteMetric(metrics)
}

func (d *DAGAdapter) calculateIngressRouteMetric() metrics.IngressRouteMetric {
	metricTotal := make(map[metrics.Meta]int)
	metricValid := make(map[metrics.Meta]int)
	metricInvalid := make(map[metrics.Meta]int)
	metricOrphaned := make(map[metrics.Meta]int)
	metricRoots := make(map[metrics.Meta]int)

	for _, v := range d.Statuses() {
		switch v.Status {
		case dag.StatusValid:
			metricValid[metrics.Meta{VHost: v.Vhost, Namespace: v.Object.GetNamespace()}]++
		case dag.StatusInvalid:
			metricInvalid[metrics.Meta{VHost: v.Vhost, Namespace: v.Object.GetNamespace()}]++
		case dag.StatusOrphaned:
			metricOrphaned[metrics.Meta{Namespace: v.Object.GetNamespace()}]++
		}
		metricTotal[metrics.Meta{Namespace: v.Object.GetNamespace()}]++

		if v.Object.Spec.VirtualHost != nil {
			metricRoots[metrics.Meta{Namespace: v.Object.GetNamespace()}]++
		}
	}

	return metrics.IngressRouteMetric{
		Invalid:  metricInvalid,
		Valid:    metricValid,
		Orphaned: metricOrphaned,
		Total:    metricTotal,
		Root:     metricRoots,
	}
}
