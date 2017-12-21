// Copyright © 2017 Heptio
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

package contour

import (
	"sort"
	"sync"

	v2 "github.com/envoyproxy/go-control-plane/api"
)

// clusterCache is a thread safe, atomic, copy on write cache of *v2.Cluster objects.
type clusterCache struct {
	sync.Mutex
	values []*v2.Cluster
}

// Values returns a copy of the contents of the cache.
func (cc *clusterCache) Values() []*v2.Cluster {
	cc.Lock()
	r := append([]*v2.Cluster{}, cc.values...)
	cc.Unlock()
	return r
}

// Add adds an entry to the cache. If a Cluster with the same
// name exists, it is replaced.
// TODO(dfc) make Add variadic to support atomic addition of several clusters
// also niladic Add can be used as a no-op notify for watchers.
func (cc *clusterCache) Add(c *v2.Cluster) {
	cc.Lock()
	defer cc.Unlock()
	sort.Sort(clusterByName(cc.values))
	i := sort.Search(len(cc.values), func(i int) bool { return cc.values[i].Name >= c.Name })
	if i < len(cc.values) && cc.values[i].Name == c.Name {
		// c is already present, replace
		cc.values[i] = c
	} else {
		// c is not present, append
		cc.values = append(cc.values, c)
		// restort to convert append into insert
		sort.Sort(clusterByName(cc.values))
	}
}

// Remove removes the named entry from the cache. If the entry
// is not present in the cache, the operation is a no-op.
func (cc *clusterCache) Remove(name string) {
	cc.Lock()
	defer cc.Unlock()
	sort.Sort(clusterByName(cc.values))
	i := sort.Search(len(cc.values), func(i int) bool { return cc.values[i].Name >= name })
	if i < len(cc.values) && cc.values[i].Name == name {
		// c is present, remove
		cc.values = append(cc.values[:i], cc.values[i+1:]...)
	}
}

type clusterByName []*v2.Cluster

func (c clusterByName) Len() int           { return len(c) }
func (c clusterByName) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c clusterByName) Less(i, j int) bool { return c[i].Name < c[j].Name }

// clusterLoadAssignmentCache is a thread safe, atomic, copy on write cache of v2.ClusterLoadAssignment objects.
type clusterLoadAssignmentCache struct {
	sync.Mutex
	values []*v2.ClusterLoadAssignment
}

// Values returns a copy of the contents of the cache.
func (c *clusterLoadAssignmentCache) Values() []*v2.ClusterLoadAssignment {
	c.Lock()
	r := append([]*v2.ClusterLoadAssignment{}, c.values...)
	c.Unlock()
	return r
}

// Add adds an entry to the cache. If a ClusterLoadAssignment with the same
// name exists, it is replaced.
// TODO(dfc) make Add variadic to support atomic addition of several clusterLoadAssignments
// also niladic Add can be used as a no-op notify for watchers.
func (c *clusterLoadAssignmentCache) Add(e *v2.ClusterLoadAssignment) {
	c.Lock()
	defer c.Unlock()
	sort.Sort(clusterLoadAssignmentsByName(c.values))
	i := sort.Search(len(c.values), func(i int) bool { return c.values[i].ClusterName >= e.ClusterName })
	if i < len(c.values) && c.values[i].ClusterName == e.ClusterName {
		c.values[i] = e
	} else {
		c.values = append(c.values, e)
		sort.Sort(clusterLoadAssignmentsByName(c.values))
	}
}

// Remove removes the named entry from the cache. If the entry
// is not present in the cache, the operation is a no-op.
func (c *clusterLoadAssignmentCache) Remove(name string) {
	c.Lock()
	defer c.Unlock()
	sort.Sort(clusterLoadAssignmentsByName(c.values))
	i := sort.Search(len(c.values), func(i int) bool { return c.values[i].ClusterName >= name })
	if i < len(c.values) && c.values[i].ClusterName == name {
		// c is present, remove
		c.values = append(c.values[:i], c.values[i+1:]...)
	}
}

type clusterLoadAssignmentsByName []*v2.ClusterLoadAssignment

func (c clusterLoadAssignmentsByName) Len() int           { return len(c) }
func (c clusterLoadAssignmentsByName) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c clusterLoadAssignmentsByName) Less(i, j int) bool { return c[i].ClusterName < c[j].ClusterName }

// ListenerCache is a thread safe, atomic, copy on write cache of v2.Listener objects.
type listenerCache struct {
	sync.Mutex
	values []*v2.Listener
}

// Values returns a copy of the contents of the cache.
func (lc *listenerCache) Values() []*v2.Listener {
	lc.Lock()
	r := append([]*v2.Listener{}, lc.values...)
	lc.Unlock()
	return r
}

// Add adds an entry to the cache. If a Listener with the same
// name exists, it is replaced.
// TODO(dfc) make Add variadic to support atomic addition of several listeners
// also niladic Add can be used as a no-op notify for watchers.
func (lc *listenerCache) Add(r *v2.Listener) {
	lc.Lock()
	defer lc.Unlock()
	sort.Sort(listenersByName(lc.values))
	i := sort.Search(len(lc.values), func(i int) bool { return lc.values[i].Name >= r.Name })
	if i < len(lc.values) && lc.values[i].Name == r.Name {
		// c is already present, replace
		lc.values[i] = r
	} else {
		// c is not present, append and sort
		lc.values = append(lc.values, r)
		sort.Sort(listenersByName(lc.values))
	}
}

// Remove removes the named entry from the cache. If the entry
// is not present in the cache, the operation is a no-op.
func (lc *listenerCache) Remove(name string) {
	lc.Lock()
	defer lc.Unlock()
	sort.Sort(listenersByName(lc.values))
	i := sort.Search(len(lc.values), func(i int) bool { return lc.values[i].Name >= name })
	if i < len(lc.values) && lc.values[i].Name == name {
		// c is present, remove
		lc.values = append(lc.values[:i], lc.values[i+1:]...)
	}
}

type listenersByName []*v2.Listener

func (l listenersByName) Len() int           { return len(l) }
func (l listenersByName) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }
func (l listenersByName) Less(i, j int) bool { return l[i].Name < l[j].Name }

// clusterLoadAssignmentCache is a thread safe, atomic, copy on write cache of v2.ClusterLoadAssignment objects.

// VirtualHostCache is a thread safe, atomic, copy on write cache of v2.VirtualHost objects.
type virtualHostCache struct {
	sync.Mutex
	values []*v2.VirtualHost
}

// Values returns a copy of the contents of the cache.
func (vc *virtualHostCache) Values() []*v2.VirtualHost {
	vc.Lock()
	r := append([]*v2.VirtualHost{}, vc.values...)
	vc.Unlock()
	return r
}

// Add adds an entry to the cache. If a VirtualHost with the same
// name exists, it is replaced.
// TODO(dfc) make Add variadic to support atomic addition of several clusters
// also niladic Add can be used as a no-op notify for watchers.
func (vc *virtualHostCache) Add(r *v2.VirtualHost) {
	vc.Lock()
	defer vc.Unlock()
	sort.Sort(virtualHostsByName(vc.values))
	i := sort.Search(len(vc.values), func(i int) bool { return vc.values[i].Name >= r.Name })
	if i < len(vc.values) && vc.values[i].Name == r.Name {
		// c is already present, replace
		vc.values[i] = r
	} else {
		// c is not present, append and sort
		vc.values = append(vc.values, r)
		sort.Sort(virtualHostsByName(vc.values))
	}
}

// Remove removes the named entry from the cache. If the entry
// is not present in the cache, the operation is a no-op.
func (vc *virtualHostCache) Remove(name string) {
	vc.Lock()
	defer vc.Unlock()
	sort.Sort(virtualHostsByName(vc.values))
	i := sort.Search(len(vc.values), func(i int) bool { return vc.values[i].Name >= name })
	if i < len(vc.values) && vc.values[i].Name == name {
		// c is present, remove
		vc.values = append(vc.values[:i], vc.values[i+1:]...)
	}
}

type virtualHostsByName []*v2.VirtualHost

func (v virtualHostsByName) Len() int           { return len(v) }
func (v virtualHostsByName) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }
func (v virtualHostsByName) Less(i, j int) bool { return v[i].Name < v[j].Name }
