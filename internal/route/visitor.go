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

package route

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"
	"github.com/heptio/contour/internal/dag"
)

type Visitor struct {
	*RouteCache
	*dag.DAG
}

func (v *Visitor) Visit() map[string]*v2.RouteConfiguration {
	ingress_http := &v2.RouteConfiguration{
		Name: "ingress_http",
	}
	ingress_https := &v2.RouteConfiguration{
		Name: "ingress_https",
	}
	m := map[string]*v2.RouteConfiguration{
		ingress_http.Name:  ingress_http,
		ingress_https.Name: ingress_https,
	}
	v.DAG.Visit(func(vh dag.Vertex) {
		switch vh := vh.(type) {
		case *dag.VirtualHost:
			hostname := vh.FQDN()
			domains := []string{hostname}
			if hostname != "*" {
				domains = append(domains, hostname+":80")
			}
			vhost := route.VirtualHost{
				Name:    hashname(60, hostname),
				Domains: domains,
			}
			vh.Visit(func(r dag.Vertex) {
				switch r := r.(type) {
				case *dag.Route:
					var svcs []*dag.Service
					r.Visit(func(s dag.Vertex) {
						if s, ok := s.(*dag.Service); ok {
							svcs = append(svcs, s)
						}
					})
					if len(svcs) < 1 {
						// no services for this route, skip it.
						return
					}
					rr := route.Route{
						Match: prefixmatch(r.Prefix()),
						Action: actionroute(
							svcs[0].Namespace(),
							svcs[0].Name(),
							svcs[0].Port, // TODO(dfc) support more than one weighted service
							r.Websocket,
							r.Timeout),
					}

					if r.HTTPSUpgrade {
						rr.Action = &route.Route_Redirect{
							Redirect: &route.RedirectAction{
								HttpsRedirect: true,
							},
						}
					}
					vhost.Routes = append(vhost.Routes, rr)
				}
			})
			if len(vhost.Routes) < 1 {
				return
			}
			sort.Stable(sort.Reverse(longestRouteFirst(vhost.Routes)))
			ingress_http.VirtualHosts = append(ingress_http.VirtualHosts, vhost)
		case *dag.SecureVirtualHost:
			hostname := vh.FQDN()
			domains := []string{hostname}
			if hostname != "*" {
				domains = append(domains, hostname+":443")
			}
			vhost := route.VirtualHost{
				Name:    hashname(60, hostname),
				Domains: domains,
			}
			vh.Visit(func(r dag.Vertex) {
				switch r := r.(type) {
				case *dag.Route:
					var svcs []*dag.Service
					r.Visit(func(s dag.Vertex) {
						if s, ok := s.(*dag.Service); ok {
							svcs = append(svcs, s)
						}
					})
					if len(svcs) < 1 {
						// no services for this route, skip it.
						fmt.Printf("no services for %v:%d%s\n", hostname, 443, r.Prefix())
						return
					}
					vhost.Routes = append(vhost.Routes, route.Route{
						Match: prefixmatch(r.Prefix()),
						Action: actionroute(
							svcs[0].Namespace(),
							svcs[0].Name(),
							svcs[0].Port,
							r.Websocket,
							r.Timeout),
					})
				}
			})
			if len(vhost.Routes) < 1 {
				fmt.Printf("no routes for %v:%d\n", hostname, 443)
				return
			}
			sort.Stable(sort.Reverse(longestRouteFirst(vhost.Routes)))
			ingress_https.VirtualHosts = append(ingress_https.VirtualHosts, vhost)
		}
	})
	return m
}

type longestRouteFirst []route.Route

func (l longestRouteFirst) Len() int      { return len(l) }
func (l longestRouteFirst) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
func (l longestRouteFirst) Less(i, j int) bool {
	a, ok := l[i].Match.PathSpecifier.(*route.RouteMatch_Prefix)
	if !ok {
		// ignore non prefix matches
		return false
	}

	b, ok := l[j].Match.PathSpecifier.(*route.RouteMatch_Prefix)
	if !ok {
		// ignore non prefix matches
		return false
	}

	return a.Prefix < b.Prefix
}

// prefixmatch returns a RouteMatch for the supplied prefix.
func prefixmatch(prefix string) route.RouteMatch {
	return route.RouteMatch{
		PathSpecifier: &route.RouteMatch_Prefix{
			Prefix: prefix,
		},
	}
}

// action computes the cluster route action, a *route.Route_route for the
// supplied ingress and backend.
func actionroute(namespace, name string, port int, ws bool, timeout time.Duration) *route.Route_Route {
	cluster := hashname(60, namespace, name, strconv.Itoa(port))
	rr := route.Route_Route{
		Route: &route.RouteAction{
			ClusterSpecifier: &route.RouteAction_Cluster{
				Cluster: cluster,
			},
		},
	}
	if ws {
		rr.Route.UseWebsocket = &types.BoolValue{Value: ws}
	}
	switch timeout {
	case 0:
		// no timeout specified, do nothing
	case -1:
		// infinite timeout, set timeout value to a pointer to zero which tells
		// envoy "infinite timeout"
		infinity := time.Duration(0)
		rr.Route.Timeout = &infinity
	default:
		rr.Route.Timeout = &timeout
	}

	return &rr
}

// hashname takes a lenth l and a varargs of strings s and returns a string whose length
// which does not exceed l. Internally s is joined with strings.Join(s, "/"). If the
// combined length exceeds l then hashname truncates each element in s, starting from the
// end using a hash derived from the contents of s (not the current element). This process
// continues until the length of s does not exceed l, or all elements have been truncated.
// In which case, the entire string is replaced with a hash not exceeding the length of l.
func hashname(l int, s ...string) string {
	const shorthash = 6 // the length of the shorthash

	r := strings.Join(s, "/")
	if l > len(r) {
		// we're under the limit, nothing to do
		return r
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(r)))
	for n := len(s) - 1; n >= 0; n-- {
		s[n] = truncate(l/len(s), s[n], hash[:shorthash])
		r = strings.Join(s, "/")
		if l > len(r) {
			return r
		}
	}
	// truncated everything, but we're still too long
	// just return the hash truncated to l.
	return hash[:min(len(hash), l)]
}

// truncate truncates s to l length by replacing the
// end of s with -suffix.
func truncate(l int, s, suffix string) string {
	if l >= len(s) {
		// under the limit, nothing to do
		return s
	}
	if l <= len(suffix) {
		// easy case, just return the start of the suffix
		return suffix[:min(l, len(suffix))]
	}
	return s[:l-len(suffix)-1] + "-" + suffix
}

func min(a, b int) int {
	if a > b {
		return b
	}
	return a
}
