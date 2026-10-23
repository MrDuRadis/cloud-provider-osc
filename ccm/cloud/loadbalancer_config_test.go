/*
SPDX-FileCopyrightText: 2025 Outscale SAS <opensource@outscale.com>

SPDX-License-Identifier: BSD-3-Clause
*/
package cloud_test

import (
	"maps"
	"testing"

	"github.com/outscale/cloud-provider-osc/ccm/cloud"
	"github.com/outscale/goutils/sdk/ptr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilnet "k8s.io/utils/net"
)

func ipNetSet(nets ...string) utilnet.IPNetSet {
	set, _ := utilnet.ParseIPNets(nets...)
	return set
}

func TestLoadBalancerNamesFromService(t *testing.T) {
	tests := []struct {
		name        string
		uid         string
		annotations map[string]string
		want        []string
		wantErr     bool
	}{
		{
			name: "UID name",
			uid:  "01234567-89ab-cdef-0123-456789abcdef",
			want: []string{"0123456789abcdef0123456789abcdef"},
		},
		{
			name: "OSC annotated multi-instance names",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-name":      "name-a,name-b",
				"service.beta.kubernetes.io/osc-load-balancer-instances": "2",
			},
			want: []string{"name-a", "name-b"},
		},
		{
			name: "AWS annotation aliases",
			annotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-name":      "name-a,name-b",
				"service.beta.kubernetes.io/aws-load-balancer-instances": "2",
			},
			want: []string{"name-a", "name-b"},
		},
		{
			name: "generated multi-instance names are truncated",
			uid:  "5dbb147b-3878-4d10-bf2d-96b2b463b8eb",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-instances": "2",
			},
			want: []string{"5dbb147b38784d10bf2d96b2b463b8-a", "5dbb147b38784d10bf2d96b2b463b8-b"},
		},
		{
			name: "unrelated invalid annotation is ignored",
			uid:  "012-345",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-healthcheck-port": "invalid",
			},
			want: []string{"012345"},
		},
		{
			name: "too few configured names",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-name":      "name-a",
				"service.beta.kubernetes.io/osc-load-balancer-instances": "2",
			},
			wantErr: true,
		},
		{
			name: "too many OSC configured names",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-name":      "name-a,name-b",
				"service.beta.kubernetes.io/osc-load-balancer-instances": "1",
			},
			wantErr: true,
		},
		{
			name: "too many AWS configured names",
			annotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-name":      "name-a,name-b",
				"service.beta.kubernetes.io/aws-load-balancer-instances": "1",
			},
			wantErr: true,
		},
		{
			name: "instances above supported range",
			uid:  "012-345",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-instances": "4",
			},
			wantErr: true,
		},
		{
			name: "negative instances",
			uid:  "012-345",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-instances": "-1",
			},
			wantErr: true,
		},
		{
			name: "huge instances",
			uid:  "012-345",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-instances": "999999999999999999",
			},
			wantErr: true,
		},
		{
			name: "empty UID for generated multi-instance names",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-instances": "2",
			},
			wantErr: true,
		},
		{
			name: "conflicting OSC and AWS names",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-name": "name-a",
				"service.beta.kubernetes.io/aws-load-balancer-name": "name-b",
			},
			wantErr: true,
		},
		{
			name: "conflicting OSC and AWS instance counts",
			uid:  "012-345",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-instances": "1",
				"service.beta.kubernetes.io/aws-load-balancer-instances": "2",
			},
			wantErr: true,
		},
		{
			name: "duplicate configured names",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-name":      "name-a,name-a",
				"service.beta.kubernetes.io/osc-load-balancer-instances": "2",
			},
			wantErr: true,
		},
		{
			name: "duplicate names after truncation",
			annotations: map[string]string{
				"service.beta.kubernetes.io/osc-load-balancer-name":      "nametotallyandabsolutelytoolong-foo,nametotallyandabsolutelytoolong-bar",
				"service.beta.kubernetes.io/osc-load-balancer-instances": "2",
			},
			wantErr: true,
		},
		{
			name:    "empty identity",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: types.UID(tt.uid), Annotations: maps.Clone(tt.annotations)}}
			original := svc.DeepCopy()

			got, err := cloud.LoadBalancerNamesFromService(svc)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
			assert.Equal(t, original, svc)
		})
	}
}

func TestNewLoadBalancerUsesTheSameIdentityValidation(t *testing.T) {
	tests := []map[string]string{
		{
			"service.beta.kubernetes.io/osc-load-balancer-name": "name-a",
			"service.beta.kubernetes.io/aws-load-balancer-name": "name-b",
		},
		{
			"service.beta.kubernetes.io/osc-load-balancer-instances": "1",
			"service.beta.kubernetes.io/aws-load-balancer-instances": "2",
		},
		{
			"service.beta.kubernetes.io/osc-load-balancer-name":      "name-a,name-b",
			"service.beta.kubernetes.io/osc-load-balancer-instances": "1",
		},
		{
			"service.beta.kubernetes.io/aws-load-balancer-name":      "name-a,name-b",
			"service.beta.kubernetes.io/aws-load-balancer-instances": "1",
		},
	}

	for _, annotations := range tests {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{UID: "012-345", Annotations: annotations},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP}},
			},
		}
		_, namesErr := cloud.LoadBalancerNamesFromService(svc)
		_, buildErr := cloud.NewLoadBalancer(svc, nil)
		require.Error(t, namesErr)
		require.Error(t, buildErr)
	}
}

func TestNewLoadBalancer(t *testing.T) {
	tcs := []struct {
		name string
		svc  *corev1.Service
		tags map[string]string
		lb   *cloud.LoadBalancer
		err  bool
	}{{
		name: "a simple LBU with default values",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		lb: &cloud.LoadBalancer{
			Name:        []string{"0123456789"},
			Instances:   1,
			ServiceName: "foo",
			Listeners: []cloud.Listener{{
				Port:        42,
				BackendPort: 43,
			}},
			ListenerDefaults: cloud.ListenerDefaults{
				SSLPorts: []string{"*"},
			},
			TargetRole: cloud.DefaultLoadBalancerConfiguration.TargetRole,
			HealthCheck: cloud.HealthCheck{
				Port:               43,
				Protocol:           "tcp",
				Interval:           cloud.DefaultLoadBalancerConfiguration.HealthCheck.Interval,
				Timeout:            cloud.DefaultLoadBalancerConfiguration.HealthCheck.Timeout,
				HealthyThreshold:   cloud.DefaultLoadBalancerConfiguration.HealthCheck.HealthyThreshold,
				UnhealthyThreshold: cloud.DefaultLoadBalancerConfiguration.HealthCheck.UnhealthyThreshold,
			},
			AllowFrom:      ipNetSet("0.0.0.0/0"),
			Connection:     cloud.Connection{IdleTimeout: 60},
			IngressAddress: cloud.Hostname,
		},
	}, {
		name: "autogenerated names are truncated to 32 chars (non multi-az)",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "5dbb147b-3878-4d10-bf2d-96b2b463b8eb",
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		lb: &cloud.LoadBalancer{
			Name:        []string{"5dbb147b38784d10bf2d96b2b463b8eb"},
			Instances:   1,
			ServiceName: "foo",
			Listeners: []cloud.Listener{{
				Port:        42,
				BackendPort: 43,
			}},
			ListenerDefaults: cloud.ListenerDefaults{
				SSLPorts: []string{"*"},
			},
			TargetRole: cloud.DefaultLoadBalancerConfiguration.TargetRole,
			HealthCheck: cloud.HealthCheck{
				Port:               43,
				Protocol:           "tcp",
				Interval:           cloud.DefaultLoadBalancerConfiguration.HealthCheck.Interval,
				Timeout:            cloud.DefaultLoadBalancerConfiguration.HealthCheck.Timeout,
				HealthyThreshold:   cloud.DefaultLoadBalancerConfiguration.HealthCheck.HealthyThreshold,
				UnhealthyThreshold: cloud.DefaultLoadBalancerConfiguration.HealthCheck.UnhealthyThreshold,
			},
			AllowFrom:      ipNetSet("0.0.0.0/0"),
			Connection:     cloud.Connection{IdleTimeout: 60},
			IngressAddress: cloud.Hostname,
		},
	}, {
		name: "autogenerated names are truncated to 32 chars (multi-az)",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "5dbb147b-3878-4d10-bf2d-96b2b463b8eb",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-instances": "2",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		lb: &cloud.LoadBalancer{
			Name:        []string{"5dbb147b38784d10bf2d96b2b463b8-a", "5dbb147b38784d10bf2d96b2b463b8-b"},
			Instances:   2,
			ServiceName: "foo",
			Listeners: []cloud.Listener{{
				Port:        42,
				BackendPort: 43,
			}},
			ListenerDefaults: cloud.ListenerDefaults{
				SSLPorts: []string{"*"},
			},
			TargetRole: cloud.DefaultLoadBalancerConfiguration.TargetRole,
			HealthCheck: cloud.HealthCheck{
				Port:               43,
				Protocol:           "tcp",
				Interval:           cloud.DefaultLoadBalancerConfiguration.HealthCheck.Interval,
				Timeout:            cloud.DefaultLoadBalancerConfiguration.HealthCheck.Timeout,
				HealthyThreshold:   cloud.DefaultLoadBalancerConfiguration.HealthCheck.HealthyThreshold,
				UnhealthyThreshold: cloud.DefaultLoadBalancerConfiguration.HealthCheck.UnhealthyThreshold,
			},
			AllowFrom:      ipNetSet("0.0.0.0/0"),
			Connection:     cloud.Connection{IdleTimeout: 60},
			IngressAddress: cloud.Hostname,
		},
	}, {
		name: "Annotations are loaded",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":                            "name-foo",
					"service.beta.kubernetes.io/osc-load-balancer-internal":                        "true",
					"service.beta.kubernetes.io/osc-load-balancer-ip-pool":                         "pool-foo",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":                           "ip-foo",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":                       "subnet-foo",
					"service.beta.kubernetes.io/osc-load-balancer-security-group":                  "sg-foo",
					"service.beta.kubernetes.io/osc-load-balancer-extra-security-groups":           "sg-bar,sg-baz",
					"service.beta.kubernetes.io/osc-load-balancer-additional-resource-tags":        "foo=bar,foobar=barbar",
					"service.beta.kubernetes.io/osc-load-balancer-target-role":                     "role-foo",
					"service.beta.kubernetes.io/osc-load-balancer-proxy-protocol":                  "*",
					"service.beta.kubernetes.io/osc-load-balancer-backend-protocol":                "protocol-foo",
					"service.beta.kubernetes.io/osc-load-balancer-ssl-cert":                        "orn:foo",
					"service.beta.kubernetes.io/osc-load-balancer-ssl-ports":                       "443,8443",
					"service.beta.kubernetes.io/osc-load-balancer-healthcheck-healthy-threshold":   "42",
					"service.beta.kubernetes.io/osc-load-balancer-healthcheck-unhealthy-threshold": "43",
					"service.beta.kubernetes.io/osc-load-balancer-healthcheck-timeout":             "44",
					"service.beta.kubernetes.io/osc-load-balancer-healthcheck-interval":            "45",
					"service.beta.kubernetes.io/osc-load-balancer-healthcheck-port":                "46",
					"service.beta.kubernetes.io/osc-load-balancer-healthcheck-protocol":            "ssl",
					"service.beta.kubernetes.io/osc-load-balancer-healthcheck-path":                "/healthz",
					"service.beta.kubernetes.io/osc-load-balancer-access-log-enabled":              "true",
					"service.beta.kubernetes.io/osc-load-balancer-access-log-emit-interval":        "60",
					"service.beta.kubernetes.io/osc-load-balancer-access-log-oos-bucket-name":      "bucket-foo",
					"service.beta.kubernetes.io/osc-load-balancer-access-log-oos-bucket-prefix":    "prefix-foo",
					"service.beta.kubernetes.io/osc-load-balancer-connection-draining-enabled":     "true",
					"service.beta.kubernetes.io/osc-load-balancer-connection-draining-timeout":     "47",
					"service.beta.kubernetes.io/osc-load-balancer-connection-idle-timeout":         "48",
					"service.beta.kubernetes.io/osc-load-balancer-ingress-address":                 "both",
					"service.beta.kubernetes.io/load-balancer-source-ranges":                       "192.0.2.0/24,198.51.100.0/24",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		lb: &cloud.LoadBalancer{
			Name:                     []string{"name-foo"},
			ServiceName:              "foo",
			Instances:                1,
			Internal:                 true,
			PublicIPPool:             "pool-foo",
			PublicIPID:               []string{"ip-foo"},
			SubnetID:                 []string{"subnet-foo"},
			SecurityGroups:           []string{"sg-foo"},
			AdditionalSecurityGroups: []string{"sg-bar", "sg-baz"},
			TargetRole:               "role-foo",
			Listeners: []cloud.Listener{{
				Port:        42,
				BackendPort: 43,
			}},
			ListenerDefaults: cloud.ListenerDefaults{
				SSLCertificate:  "orn:foo",
				SSLPorts:        []string{"443", "8443"},
				BackendProtocol: "protocol-foo",
				ProxyProtocol:   []string{"*"},
			},
			HealthCheck: cloud.HealthCheck{
				Interval:           45,
				Timeout:            44,
				HealthyThreshold:   42,
				UnhealthyThreshold: 43,
				Port:               46,
				Protocol:           "ssl",
				Path:               "/healthz",
			},
			Connection: cloud.Connection{
				ConnectionDraining:        true,
				ConnectionDrainingTimeout: 47,
				IdleTimeout:               48,
			},
			AccessLog: cloud.AccessLog{
				Enabled:      true,
				EmitInterval: 60,
				BucketName:   "bucket-foo",
				BucketPrefix: "prefix-foo",
			},
			Tags: map[string]string{
				"foo":    "bar",
				"foobar": "barbar",
			},
			AllowFrom:      ipNetSet("192.0.2.0/24", "198.51.100.0/24"),
			IngressAddress: cloud.Both,
			IPMode:         ptr.To(corev1.LoadBalancerIPModeProxy),
		},
	}, {
		name: "Tags are merged with the global config",
		tags: map[string]string{
			"foobar": "bazbar",
			"foobaz": "bazbaz",
		},
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-additional-resource-tags": "foo=bar,foobar=barbar",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		lb: &cloud.LoadBalancer{
			Name:        []string{"0123456789"},
			ServiceName: "foo",
			Instances:   1,
			Listeners: []cloud.Listener{{
				Port:        42,
				BackendPort: 43,
			}},
			ListenerDefaults: cloud.ListenerDefaults{
				SSLPorts: []string{"*"},
			},
			TargetRole: cloud.DefaultLoadBalancerConfiguration.TargetRole,
			HealthCheck: cloud.HealthCheck{
				Port:               43,
				Protocol:           "tcp",
				Interval:           cloud.DefaultLoadBalancerConfiguration.HealthCheck.Interval,
				Timeout:            cloud.DefaultLoadBalancerConfiguration.HealthCheck.Timeout,
				HealthyThreshold:   cloud.DefaultLoadBalancerConfiguration.HealthCheck.HealthyThreshold,
				UnhealthyThreshold: cloud.DefaultLoadBalancerConfiguration.HealthCheck.UnhealthyThreshold,
			},
			AllowFrom:  ipNetSet("0.0.0.0/0"),
			Connection: cloud.Connection{IdleTimeout: 60},
			Tags: map[string]string{
				"foo":    "bar",
				"foobar": "barbar",
				"foobaz": "bazbaz",
			},
			IngressAddress: cloud.Hostname,
		},
	}, {
		name: "AWS annotations are loaded",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/aws-load-balancer-name":                            "name-foo",
					"service.beta.kubernetes.io/aws-load-balancer-internal":                        "true",
					"service.beta.kubernetes.io/aws-load-balancer-subnet-id":                       "subnet-foo",
					"service.beta.kubernetes.io/aws-load-balancer-security-groups":                 "sg-foo",
					"service.beta.kubernetes.io/aws-load-balancer-extra-security-groups":           "sg-bar,sg-baz",
					"service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags":        "foo=bar,foobar=barbar",
					"service.beta.kubernetes.io/aws-load-balancer-target-role":                     "role-foo",
					"service.beta.kubernetes.io/aws-load-balancer-proxy-protocol":                  "*",
					"service.beta.kubernetes.io/aws-load-balancer-backend-protocol":                "protocol-foo",
					"service.beta.kubernetes.io/aws-load-balancer-ssl-cert":                        "orn:foo",
					"service.beta.kubernetes.io/aws-load-balancer-ssl-ports":                       "443,8443",
					"service.beta.kubernetes.io/aws-load-balancer-healthcheck-healthy-threshold":   "42",
					"service.beta.kubernetes.io/aws-load-balancer-healthcheck-unhealthy-threshold": "43",
					"service.beta.kubernetes.io/aws-load-balancer-healthcheck-timeout":             "44",
					"service.beta.kubernetes.io/aws-load-balancer-healthcheck-interval":            "45",
					"service.beta.kubernetes.io/aws-load-balancer-healthcheck-port":                "46",
					"service.beta.kubernetes.io/aws-load-balancer-healthcheck-protocol":            "ssl",
					"service.beta.kubernetes.io/aws-load-balancer-healthcheck-path":                "/healthz",
					"service.beta.kubernetes.io/aws-load-balancer-access-log-enabled":              "true",
					"service.beta.kubernetes.io/aws-load-balancer-access-log-emit-interval":        "60",
					"service.beta.kubernetes.io/aws-load-balancer-access-log-s3-bucket-name":       "bucket-foo",
					"service.beta.kubernetes.io/aws-load-balancer-access-log-s3-bucket-prefix":     "prefix-foo",
					"service.beta.kubernetes.io/aws-load-balancer-connection-draining-enabled":     "true",
					"service.beta.kubernetes.io/aws-load-balancer-connection-draining-timeout":     "47",
					"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout":         "48",
					"service.beta.kubernetes.io/load-balancer-source-ranges":                       "192.0.2.0/24,198.51.100.0/24",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		lb: &cloud.LoadBalancer{
			Name:                     []string{"name-foo"},
			ServiceName:              "foo",
			Instances:                1,
			Internal:                 true,
			SubnetID:                 []string{"subnet-foo"},
			SecurityGroups:           []string{"sg-foo"},
			AdditionalSecurityGroups: []string{"sg-bar", "sg-baz"},
			TargetRole:               "role-foo",
			Listeners: []cloud.Listener{{
				Port:        42,
				BackendPort: 43,
			}},
			ListenerDefaults: cloud.ListenerDefaults{
				SSLCertificate:  "orn:foo",
				SSLPorts:        []string{"443", "8443"},
				BackendProtocol: "protocol-foo",
				ProxyProtocol:   []string{"*"},
			},
			HealthCheck: cloud.HealthCheck{
				Interval:           45,
				Timeout:            44,
				HealthyThreshold:   42,
				UnhealthyThreshold: 43,
				Port:               46,
				Protocol:           "ssl",
				Path:               "/healthz",
			},
			Connection: cloud.Connection{
				ConnectionDraining:        true,
				ConnectionDrainingTimeout: 47,
				IdleTimeout:               48,
			},
			AccessLog: cloud.AccessLog{
				Enabled:      true,
				EmitInterval: 60,
				BucketName:   "bucket-foo",
				BucketPrefix: "prefix-foo",
			},
			Tags: map[string]string{
				"foo":    "bar",
				"foobar": "barbar",
			},
			AllowFrom:      ipNetSet("192.0.2.0/24", "198.51.100.0/24"),
			IngressAddress: cloud.Hostname,
		},
	}, {
		name: "Source ranges can be set in the spec",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity:          corev1.ServiceAffinityNone,
				Ports:                    []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
				LoadBalancerSourceRanges: []string{"192.0.2.0/24", "198.51.100.0/24"},
			},
		},
		lb: &cloud.LoadBalancer{
			Name:        []string{"0123456789"},
			ServiceName: "foo",
			Instances:   1,
			Listeners: []cloud.Listener{{
				Port:        42,
				BackendPort: 43,
			}},
			ListenerDefaults: cloud.ListenerDefaults{
				SSLPorts: []string{"*"},
			},
			TargetRole: cloud.DefaultLoadBalancerConfiguration.TargetRole,
			HealthCheck: cloud.HealthCheck{
				Port:               43,
				Protocol:           "tcp",
				Interval:           cloud.DefaultLoadBalancerConfiguration.HealthCheck.Interval,
				Timeout:            cloud.DefaultLoadBalancerConfiguration.HealthCheck.Timeout,
				HealthyThreshold:   cloud.DefaultLoadBalancerConfiguration.HealthCheck.HealthyThreshold,
				UnhealthyThreshold: cloud.DefaultLoadBalancerConfiguration.HealthCheck.UnhealthyThreshold,
			},
			AllowFrom:      ipNetSet("192.0.2.0/24", "198.51.100.0/24"),
			Connection:     cloud.Connection{IdleTimeout: 60},
			IngressAddress: cloud.Hostname,
		},
	}, {
		name: "Multi-AZ LBUs can be configured",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":       "name-foo,name-bar",
					"service.beta.kubernetes.io/osc-load-balancer-instances":  "2",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":      "ipalloc-foo,ipalloc-bar",
					"service.beta.kubernetes.io/osc-load-balancer-subregions": "eu-west-2a,eu-west-2b",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":  "subnet-foo,subnet-bar",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		lb: &cloud.LoadBalancer{
			Name:        []string{"name-foo", "name-bar"},
			ServiceName: "foo",
			Instances:   2,
			PublicIPID:  []string{"ipalloc-foo", "ipalloc-bar"},
			SubnetID:    []string{"subnet-foo", "subnet-bar"},
			SubRegions:  []string{"eu-west-2a", "eu-west-2b"},
			Listeners: []cloud.Listener{{
				Port:        42,
				BackendPort: 43,
			}},
			ListenerDefaults: cloud.ListenerDefaults{
				SSLPorts: []string{"*"},
			},
			TargetRole: cloud.DefaultLoadBalancerConfiguration.TargetRole,
			HealthCheck: cloud.HealthCheck{
				Port:               43,
				Protocol:           "tcp",
				Interval:           cloud.DefaultLoadBalancerConfiguration.HealthCheck.Interval,
				Timeout:            cloud.DefaultLoadBalancerConfiguration.HealthCheck.Timeout,
				HealthyThreshold:   cloud.DefaultLoadBalancerConfiguration.HealthCheck.HealthyThreshold,
				UnhealthyThreshold: cloud.DefaultLoadBalancerConfiguration.HealthCheck.UnhealthyThreshold,
			},
			Connection:     cloud.Connection{IdleTimeout: 60},
			IngressAddress: cloud.Hostname,
			AllowFrom:      ipNetSet("0.0.0.0/0"),
		},
	}, {
		name: "instances cannot be changed on a configured service",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":       "name-foo,name-bar",
					"service.beta.kubernetes.io/osc-load-balancer-instances":  "2",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":      "ipalloc-foo,ipalloc-bar",
					"service.beta.kubernetes.io/osc-load-balancer-subregions": "eu-west-2a,eu-west-2b",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":  "subnet-foo,subnet-bar",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{{Hostname: "foo.example.com"}},
				},
			},
		},
		err: true,
	}, {
		name: "The right number of names must be configured in a multi-az setup",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":       "name-foo",
					"service.beta.kubernetes.io/osc-load-balancer-instances":  "2",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":      "ipalloc-foo,ipalloc-bar",
					"service.beta.kubernetes.io/osc-load-balancer-subregions": "eu-west-2a,eu-west-2b",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":  "subnet-foo,subnet-bar",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		err: true,
	}, {
		name: "Names must be unique",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":       "name-foo,name-foo",
					"service.beta.kubernetes.io/osc-load-balancer-instances":  "2",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":      "ipalloc-foo,ipalloc-bar",
					"service.beta.kubernetes.io/osc-load-balancer-subregions": "eu-west-2a,eu-west-2b",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":  "subnet-foo,subnet-bar",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		err: true,
	}, {
		name: "Names must be unique after trucation",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":       "nametotallyandabsolutelytoolong-foo,nametotallyandabsolutelytoolong-bar",
					"service.beta.kubernetes.io/osc-load-balancer-instances":  "2",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":      "ipalloc-foo,ipalloc-bar",
					"service.beta.kubernetes.io/osc-load-balancer-subregions": "eu-west-2a,eu-west-2b",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":  "subnet-foo,subnet-bar",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		err: true,
	}, {
		name: "The right number of ips must be configured in a multi-az setup",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":       "name-foo,name-bar",
					"service.beta.kubernetes.io/osc-load-balancer-instances":  "2",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":      "ipalloc-foo",
					"service.beta.kubernetes.io/osc-load-balancer-subregions": "eu-west-2a,eu-west-2b",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":  "subnet-foo,subnet-bar",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		err: true,
	}, {
		name: "The right number of subregions must be configured in a multi-az setup",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":       "name-foo,name-bar",
					"service.beta.kubernetes.io/osc-load-balancer-instances":  "2",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":      "ipalloc-foo,ipalloc-bar",
					"service.beta.kubernetes.io/osc-load-balancer-subregions": "eu-west-2a",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":  "subnet-foo,subnet-bar",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		err: true,
	}, {
		name: "The right number of subnets must be configured in a multi-az setup",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				UID:  "012-3456-789",
				Annotations: map[string]string{
					"service.beta.kubernetes.io/osc-load-balancer-name":       "name-foo,name-bar",
					"service.beta.kubernetes.io/osc-load-balancer-instances":  "2",
					"service.beta.kubernetes.io/osc-load-balancer-ip-id":      "ipalloc-foo,ipalloc-bar",
					"service.beta.kubernetes.io/osc-load-balancer-subregions": "eu-west-2a,eu-west-2b",
					"service.beta.kubernetes.io/osc-load-balancer-subnet-id":  "subnet-foo",
				},
			},
			Spec: corev1.ServiceSpec{
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP, Port: 42, NodePort: 43}},
			},
		},
		err: true,
	}}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.svc.DeepCopy()
			tags := tc.tags
			if tags == nil {
				tags = map[string]string{}
			}
			lb, err := cloud.NewLoadBalancer(tc.svc, tags)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.lb, lb)
			}
			assert.Equal(t, original, tc.svc)
		})
	}
}
