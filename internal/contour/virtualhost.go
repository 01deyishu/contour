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
	"strings"
	"time"

	v2 "github.com/envoyproxy/go-control-plane/api"
	"k8s.io/api/extensions/v1beta1"
)

// VirtualHostCache manage the contents of the gRPC RDS cache.
type VirtualHostCache struct {
	HTTP  virtualHostCache
	HTTPS virtualHostCache
	Cond
}

const (
	requestTimeout = "contour.heptio.com/request-timeout"

	// By default envoy applies a 15 second timeout to all backend requests.
	// The explicit value 0 turns off the timeout, implying "never time out"
	// https://www.envoyproxy.io/docs/envoy/v1.5.0/api-v2/rds.proto#routeaction
	infiniteTimeout = time.Duration(0)
)

// getRequestTimeout parses the annotations map for a contour.heptio.com/request-timeout
// value. If the value is not present, false is returned and the timeout value should be
// ignored. If the value is present, but malformed, the timeout value is valid, and represents
// infinite timeout.
func getRequestTimeout(annotations map[string]string) (time.Duration, bool) {
	timeoutStr, ok := annotations[requestTimeout]
	// Error or unspecified is interpreted as no timeout specified, use envoy defaults
	if !ok || timeoutStr == "" {
		return 0, false
	}

	// Interpret "infinity" explicitly as an infinite timeout, which envoy config
	// expects as a timeout of 0. This could be specified with the duration string
	// "0s" but want to give an explicit out for operators.
	if timeoutStr == "infinity" {
		return infiniteTimeout, true
	}

	timeoutParsed, err := time.ParseDuration(timeoutStr)
	if err != nil {
		// TODO(cmalonty) plumb a logger in here so we can log this error.
		// Assuming infinite duration is going to surprise people less for
		// a not-parseable duration than a implicit 15 second one.
		return infiniteTimeout, true
	}
	return timeoutParsed, true
}

// recomputevhost recomputes the ingress_http (HTTP) and ingress_https (HTTPS) record
// from the vhost from list of ingresses supplied.
func (v *VirtualHostCache) recomputevhost(vhost string, ingresses map[metadata]*v1beta1.Ingress) {

	// handle ingress_https (TLS) vhost routes first.
	vv := virtualhost(vhost)
	for _, ing := range ingresses {
		if !validTLSSpecforVhost(vhost, ing) {
			continue
		}
		for _, rule := range ing.Spec.Rules {
			if rule.Host != "" && rule.Host != vhost {
				continue
			}
			if rule.IngressRuleValue.HTTP == nil {
				// TODO(dfc) plumb a logger in here so we can log this error.
				continue
			}

			for _, p := range rule.IngressRuleValue.HTTP.Paths {
				m := pathToRouteMatch(p)
				cl := ingressBackendToClusterName(ing, &p.Backend)
				if timeout, ok := getRequestTimeout(ing.Annotations); ok {
					a := clusteractiontimeout(cl, timeout)
					vv.Routes = append(vv.Routes, &v2.Route{Match: m, Action: a})
				} else {
					a := clusteraction(cl)
					vv.Routes = append(vv.Routes, &v2.Route{Match: m, Action: a})
				}
			}
		}
	}
	if len(vv.Routes) > 0 {
		sort.Stable(sort.Reverse(longestRouteFirst(vv.Routes)))
		v.HTTPS.Add(vv)
	} else {
		v.HTTPS.Remove(vv.Name)
	}

	// now handle ingress_http (non tls) routes.
	vv = virtualhost(vhost)
	for _, i := range ingresses {
		if i.Annotations["kubernetes.io/ingress.allow-http"] == "false" {
			// skip this vhosts ingress_http route.
			continue
		}
		if i.Annotations["ingress.kubernetes.io/force-ssl-redirect"] == "true" {
			// set blanket 301 redirect
			vv.RequireTls = v2.VirtualHost_ALL
		}
		if i.Spec.Backend != nil && len(ingresses) == 1 {
			cl := ingressBackendToClusterName(i, i.Spec.Backend)
			if timeout, ok := getRequestTimeout(i.Annotations); ok {
				a := clusteractiontimeout(cl, timeout)
				vv.Routes = []*v2.Route{{Match: prefixmatch("/"), Action: a}}
			} else {
				a := clusteraction(cl)
				vv.Routes = []*v2.Route{{Match: prefixmatch("/"), Action: a}}
			}
			continue
		}
		for _, rule := range i.Spec.Rules {
			if rule.Host != "" && rule.Host != vhost {
				continue
			}
			if rule.IngressRuleValue.HTTP == nil {
				// TODO(dfc) plumb a logger in here so we can log this error.
				continue
			}
			for _, p := range rule.IngressRuleValue.HTTP.Paths {
				m := pathToRouteMatch(p)
				cl := ingressBackendToClusterName(i, &p.Backend)
				if timeout, ok := getRequestTimeout(i.Annotations); ok {
					a := clusteractiontimeout(cl, timeout)
					vv.Routes = append(vv.Routes, &v2.Route{Match: m, Action: a})
				} else {
					a := clusteraction(cl)
					vv.Routes = append(vv.Routes, &v2.Route{Match: m, Action: a})
				}
			}
		}
	}
	if len(vv.Routes) > 0 {
		sort.Stable(sort.Reverse(longestRouteFirst(vv.Routes)))
		v.HTTP.Add(vv)
	} else {
		v.HTTP.Remove(vv.Name)
	}
}

// validTLSSpecForVhost returns if this ingress object
// contains a TLS spec that matches the vhost supplied,
func validTLSSpecforVhost(vhost string, i *v1beta1.Ingress) bool {
	for _, tls := range i.Spec.TLS {
		if tls.SecretName == "" {
			// not a valid TLS spec without a secret for the cert.
			continue
		}

		for _, h := range tls.Hosts {
			if h == vhost {
				return true
			}
		}
	}
	return false
}

type longestRouteFirst []*v2.Route

func (l longestRouteFirst) Len() int      { return len(l) }
func (l longestRouteFirst) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
func (l longestRouteFirst) Less(i, j int) bool {
	a, ok := l[i].Match.PathSpecifier.(*v2.RouteMatch_Prefix)
	if !ok {
		// ignore non prefix matches
		return false
	}

	b, ok := l[j].Match.PathSpecifier.(*v2.RouteMatch_Prefix)
	if !ok {
		// ignore non prefix matches
		return false
	}

	return a.Prefix < b.Prefix
}

// pathToRoute converts a HTTPIngressPath to a partial v2.RouteMatch.
func pathToRouteMatch(p v1beta1.HTTPIngressPath) *v2.RouteMatch {
	if p.Path == "" {
		// If the Path is empty, the k8s spec says
		// "If unspecified, the path defaults to a catch all sending
		// traffic to the backend."
		// We map this it a catch all prefix route.
		return prefixmatch("/") // match all
	}
	// TODO(dfc) handle the case where p.Path does not start with "/"
	if strings.IndexAny(p.Path, `[(*\`) == -1 {
		// Envoy requires that regex matches match completely, wheres the
		// HTTPIngressPath.Path regex only requires a partial match. eg,
		// "/foo" matches "/" according to k8s rules, but does not match
		// according to Envoy.
		// To deal with this we handle the simple case, a Path without regex
		// characters as a Envoy prefix route.
		return prefixmatch(p.Path)
	}
	// At this point the path is a regex, which we hope is the same between k8s
	// IEEE 1003.1 POSIX regex, and Envoys Javascript regex.
	return regexmatch(p.Path)
}

// ingressBackendToClusterName renders a cluster name from an Ingress and an IngressBackend.
func ingressBackendToClusterName(i *v1beta1.Ingress, b *v1beta1.IngressBackend) string {
	return hashname(60, i.ObjectMeta.Namespace, b.ServiceName, b.ServicePort.String())
}

// prefixmatch returns a RouteMatch for the supplied prefix.
func prefixmatch(prefix string) *v2.RouteMatch {
	return &v2.RouteMatch{
		PathSpecifier: &v2.RouteMatch_Prefix{
			Prefix: prefix,
		},
	}
}

// regexmatch returns a RouteMatch for the supplied regex.
func regexmatch(regex string) *v2.RouteMatch {
	return &v2.RouteMatch{
		PathSpecifier: &v2.RouteMatch_Regex{
			Regex: regex,
		},
	}
}

// clusteraction returns a Route_Route action for the supplied cluster.
func clusteraction(cluster string) *v2.Route_Route {
	return &v2.Route_Route{
		Route: &v2.RouteAction{
			ClusterSpecifier: &v2.RouteAction_Cluster{
				Cluster: cluster,
			},
		},
	}
}

// clusteractiontimeout returns a cluster action with the specified timeout.
// A timeout of 0 means infinity. If you do not want to specify a timeout, use
// clusteraction instead.
func clusteractiontimeout(cluster string, timeout time.Duration) *v2.Route_Route {
	// TODO(cmaloney): Pull timeout off of the backend cluster annotation
	// and use it over the value retrieved from the ingress annotation if
	// specified.
	c := clusteraction(cluster)
	c.Route.Timeout = &timeout
	return c
}

func virtualhost(hostname string) *v2.VirtualHost {
	return &v2.VirtualHost{
		Name:    hashname(60, hostname),
		Domains: []string{hostname},
	}
}
