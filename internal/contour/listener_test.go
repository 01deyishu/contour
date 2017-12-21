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
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	v2 "github.com/envoyproxy/go-control-plane/api"
)

func TestRecomputeListener(t *testing.T) {
	ingress_http := listener(ENVOY_HTTP_LISTENER, "0.0.0.0", 8080)
	ingress_http.FilterChains = []*v2.FilterChain{{
		Filters: []*v2.Filter{
			httpfilter(ENVOY_HTTP_LISTENER),
		},
	}}

	tests := map[string]struct {
		ingresses map[metadata]*v1beta1.Ingress
		add       []*v2.Listener
		remove    []string
	}{
		"empty ingress map": {
			ingresses: nil,
			add:       nil,
			remove:    []string{ENVOY_HTTP_LISTENER},
		},
		"default vhost ingress": {
			ingresses: map[metadata]*v1beta1.Ingress{
				metadata{namespace: "default", name: "simple"}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "simple",
						Namespace: "default",
					},
					Spec: v1beta1.IngressSpec{
						Backend: backend("backend", intstr.FromInt(80)),
					},
				},
			},
			add: []*v2.Listener{
				ingress_http,
			},
			remove: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			add, remove := recomputeListener(tc.ingresses)
			if !reflect.DeepEqual(add, tc.add) {
				t.Errorf("add:\n\texpected: %v\n\tgot: %v", tc.add, add)
			}
			if !reflect.DeepEqual(remove, tc.remove) {
				t.Errorf("remove:\n\texpected: %v,\n\tgot: %v", tc.remove, remove)
			}
		})
	}
}

func TestRecomputeTLSListener(t *testing.T) {
	ingresss_http := listener(ENVOY_HTTPS_LISTENER, "0.0.0.0", 8443)
	ingresss_http.FilterChains = []*v2.FilterChain{{
		Filters: []*v2.Filter{
			httpfilter(ENVOY_HTTPS_LISTENER),
		},
	}}

	tests := map[string]struct {
		ingresses map[metadata]*v1beta1.Ingress
		secrets   map[metadata]*v1.Secret
		add       []*v2.Listener
		remove    []string
	}{
		"empty ingress map": {
			ingresses: nil,
			secrets:   nil,
			add:       nil,
			remove:    []string{ENVOY_HTTPS_LISTENER},
		},
		// tls is not possible for the default backend vhost because it has no name.
		"default vhost ingress": {
			ingresses: map[metadata]*v1beta1.Ingress{
				metadata{namespace: "default", name: "simple"}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "simple",
						Namespace: "default",
					},
					Spec: v1beta1.IngressSpec{
						Backend: backend("backend", intstr.FromInt(80)),
					},
				},
			},
			secrets: nil,
			add:     nil,
			remove:  []string{ENVOY_HTTPS_LISTENER},
		},
		"simple vhost, with no secret": {
			ingresses: map[metadata]*v1beta1.Ingress{
				metadata{namespace: "default", name: "simple"}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "simple",
						Namespace: "default",
					},
					Spec: v1beta1.IngressSpec{
						TLS: []v1beta1.IngressTLS{{
							Hosts:      []string{"whatever.example.com"},
							SecretName: "missing",
						}},
						Backend: backend("backend", intstr.FromInt(80)),
					},
				},
			},
			secrets: nil,
			add:     nil,
			remove:  []string{ENVOY_HTTPS_LISTENER},
		},
		"simple vhost, with secret": {
			ingresses: map[metadata]*v1beta1.Ingress{
				metadata{namespace: "default", name: "simple"}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "simple",
						Namespace: "default",
					},
					Spec: v1beta1.IngressSpec{
						TLS: []v1beta1.IngressTLS{{
							Hosts:      []string{"whatever.example.com"},
							SecretName: "secret",
						}},
						Backend: backend("backend", intstr.FromInt(80)),
					},
				},
			},
			secrets: map[metadata]*v1.Secret{
				metadata{namespace: "default", name: "secret"}: {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						v1.TLSCertKey:       []byte("certificate"),
						v1.TLSPrivateKeyKey: []byte("key"),
					},
				},
			},
			add: []*v2.Listener{{
				Name:    ENVOY_HTTPS_LISTENER,
				Address: socketaddress("0.0.0.0", 8443),
				FilterChains: []*v2.FilterChain{{
					FilterChainMatch: &v2.FilterChainMatch{
						SniDomains: []string{"whatever.example.com"},
					},
					TlsContext: tlscontext("default", "secret"),
					Filters: []*v2.Filter{
						httpfilter(ENVOY_HTTPS_LISTENER),
					},
				}},
			}},
			remove: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			add, remove := recomputeTLSListener(tc.ingresses, tc.secrets)
			if !reflect.DeepEqual(add, tc.add) {
				t.Errorf("add:\n\texpected: %v\n\tgot: %v", tc.add, add)
			}
			if !reflect.DeepEqual(remove, tc.remove) {
				t.Errorf("remove:\n\texpected: %v,\n\tgot: %v", tc.remove, remove)
			}
		})
	}
}

func TestListenerCacheRecomputeListener(t *testing.T) {
	lc := new(ListenerCache)
	assertCacheEmpty(t, lc)

	i := map[metadata]*v1beta1.Ingress{
		metadata{name: "example", namespace: "default"}: &v1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "simple",
				Namespace: "default",
			},
			Spec: v1beta1.IngressSpec{
				Backend: backend("backend", intstr.FromInt(80)),
			},
		},
	}
	lc.recomputeListeners(i, nil)
	assertCacheNotEmpty(t, lc)
}

func TestListenerCacheRecomputeTLSListener(t *testing.T) {
	lc := new(ListenerCache)
	assertCacheEmpty(t, lc)

	i := map[metadata]*v1beta1.Ingress{
		metadata{name: "example", namespace: "default"}: &v1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "simple",
				Namespace: "default",
			},
			Spec: v1beta1.IngressSpec{
				Backend: backend("backend", intstr.FromInt(80)),
			},
		},
	}
	s := make(map[metadata]*v1.Secret)
	lc.recomputeTLSListener(i, s)
	assertCacheEmpty(t, lc) // expect cache to be empty, this is not a tls enabled ingress

	i[metadata{name: "example", namespace: "default"}] = &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "simple",
			Namespace: "default",
		},
		Spec: v1beta1.IngressSpec{
			TLS: []v1beta1.IngressTLS{{
				Hosts:      []string{"whatever.example.com"},
				SecretName: "secret",
			}},
			Backend: backend("backend", intstr.FromInt(80)),
		},
	}
	lc.recomputeTLSListener(i, s)
	assertCacheEmpty(t, lc) // expect cache to be empty, this ingress is tls enabled, but missing secret

	s[metadata{name: "secret", namespace: "default"}] = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: "default",
		},
	}
	lc.recomputeTLSListener(i, s)
	assertCacheNotEmpty(t, lc) // we've got the secret and the ingress, we should have at least one listener
}

func assertCacheEmpty(t *testing.T, lc *ListenerCache) {
	t.Helper()
	if len(lc.values) > 0 {
		t.Fatalf("len(lc.values): expected 0, got %d", len(lc.values))
	}
}

func assertCacheNotEmpty(t *testing.T, lc *ListenerCache) {
	t.Helper()
	if len(lc.values) == 0 {
		t.Fatalf("len(lc.values): expected > 0, got %d", len(lc.values))
	}
}
