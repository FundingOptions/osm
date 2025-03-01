package injector

import (
	"time"

	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/openservicemesh/osm/pkg/constants"
	"github.com/openservicemesh/osm/pkg/envoy"
	"github.com/openservicemesh/osm/pkg/errcode"

	xds_accesslog_filter "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	xds_cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	xds_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xds_endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	xds_listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	xds_route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	xds_accesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/stream/v3"
	xds_http_connection_manager "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	xds_tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
)

const (
	livenessCluster  = "liveness_cluster"
	readinessCluster = "readiness_cluster"
	startupCluster   = "startup_cluster"

	livenessListener  = "liveness_listener"
	readinessListener = "readiness_listener"
	startupListener   = "startup_listener"
)

func getLivenessCluster(originalProbe *healthProbe) *xds_cluster.Cluster {
	if originalProbe == nil {
		return nil
	}
	return getProbeCluster(livenessCluster, originalProbe.port)
}

func getReadinessCluster(originalProbe *healthProbe) *xds_cluster.Cluster {
	if originalProbe == nil {
		return nil
	}
	return getProbeCluster(readinessCluster, originalProbe.port)
}

func getStartupCluster(originalProbe *healthProbe) *xds_cluster.Cluster {
	if originalProbe == nil {
		return nil
	}
	return getProbeCluster(startupCluster, originalProbe.port)
}

func getProbeCluster(clusterName string, port int32) *xds_cluster.Cluster {
	return &xds_cluster.Cluster{
		Name:           clusterName,
		ConnectTimeout: durationpb.New(time.Second),
		ClusterDiscoveryType: &xds_cluster.Cluster_Type{
			Type: xds_cluster.Cluster_STATIC,
		},
		LbPolicy: xds_cluster.Cluster_ROUND_ROBIN,
		LoadAssignment: &xds_endpoint.ClusterLoadAssignment{
			ClusterName: clusterName,
			Endpoints: []*xds_endpoint.LocalityLbEndpoints{
				{
					LbEndpoints: []*xds_endpoint.LbEndpoint{
						{
							HostIdentifier: &xds_endpoint.LbEndpoint_Endpoint{
								Endpoint: &xds_endpoint.Endpoint{
									Address: &xds_core.Address{
										Address: &xds_core.Address_SocketAddress{
											SocketAddress: &xds_core.SocketAddress{
												Address: constants.LocalhostIPAddress,
												PortSpecifier: &xds_core.SocketAddress_PortValue{
													PortValue: uint32(port),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func getLivenessListener(originalProbe *healthProbe) (*xds_listener.Listener, error) {
	if originalProbe == nil {
		return nil, nil
	}
	return getProbeListener(livenessListener, livenessCluster, livenessProbePath, livenessProbePort, originalProbe)
}

func getReadinessListener(originalProbe *healthProbe) (*xds_listener.Listener, error) {
	if originalProbe == nil {
		return nil, nil
	}
	return getProbeListener(readinessListener, readinessCluster, readinessProbePath, readinessProbePort, originalProbe)
}

func getStartupListener(originalProbe *healthProbe) (*xds_listener.Listener, error) {
	if originalProbe == nil {
		return nil, nil
	}
	return getProbeListener(startupListener, startupCluster, startupProbePath, startupProbePort, originalProbe)
}

func getProbeListener(listenerName, clusterName, newPath string, port int32, originalProbe *healthProbe) (*xds_listener.Listener, error) {
	var filterChain *xds_listener.FilterChain
	if originalProbe.isHTTP {
		httpAccessLog, err := getHTTPAccessLog()
		if err != nil {
			return nil, err
		}
		httpConnectionManager := &xds_http_connection_manager.HttpConnectionManager{
			CodecType:  xds_http_connection_manager.HttpConnectionManager_AUTO,
			StatPrefix: "health_probes_http",
			AccessLog: []*xds_accesslog_filter.AccessLog{
				httpAccessLog,
			},
			RouteSpecifier: &xds_http_connection_manager.HttpConnectionManager_RouteConfig{
				RouteConfig: &xds_route.RouteConfiguration{
					Name: "local_route",
					VirtualHosts: []*xds_route.VirtualHost{
						getVirtualHost(newPath, clusterName, originalProbe.path),
					},
				},
			},
			HttpFilters: []*xds_http_connection_manager.HttpFilter{
				{
					Name: "envoy.filters.http.router",
				},
			},
		}
		pbHTTPConnectionManager, err := ptypes.MarshalAny(httpConnectionManager)
		if err != nil {
			log.Error().Err(err).Str(errcode.Kind, errcode.GetErrCodeWithMetric(errcode.ErrMarshallingXDSResource)).
				Msgf("Error marshaling HttpConnectionManager struct into an anypb.Any message")
			return nil, err
		}
		filterChain = &xds_listener.FilterChain{
			Filters: []*xds_listener.Filter{
				{
					Name: "envoy.filters.network.http_connection_manager",
					ConfigType: &xds_listener.Filter_TypedConfig{
						TypedConfig: pbHTTPConnectionManager,
					},
				},
			},
		}
	} else {
		tcpAccessLog, err := getTCPAccessLog()
		if err != nil {
			return nil, err
		}
		tcpProxy := &xds_tcp_proxy.TcpProxy{
			StatPrefix: "health_probes",
			AccessLog: []*xds_accesslog_filter.AccessLog{
				tcpAccessLog,
			},
			ClusterSpecifier: &xds_tcp_proxy.TcpProxy_Cluster{
				Cluster: clusterName,
			},
		}
		pbTCPProxy, err := ptypes.MarshalAny(tcpProxy)
		if err != nil {
			log.Error().Err(err).Str(errcode.Kind, errcode.GetErrCodeWithMetric(errcode.ErrMarshallingXDSResource)).
				Msgf("Error marshaling TcpProxy struct into an anypb.Any message")
			return nil, err
		}
		filterChain = &xds_listener.FilterChain{
			Filters: []*xds_listener.Filter{
				{
					Name: wellknown.TCPProxy,
					ConfigType: &xds_listener.Filter_TypedConfig{
						TypedConfig: pbTCPProxy,
					},
				},
			},
		}
	}

	return &xds_listener.Listener{
		Name: listenerName,
		Address: &xds_core.Address{
			Address: &xds_core.Address_SocketAddress{
				SocketAddress: &xds_core.SocketAddress{
					Address: "0.0.0.0",
					PortSpecifier: &xds_core.SocketAddress_PortValue{
						PortValue: uint32(port),
					},
				},
			},
		},
		FilterChains: []*xds_listener.FilterChain{
			filterChain,
		},
	}, nil
}

func getVirtualHost(newPath, clusterName, originalProbePath string) *xds_route.VirtualHost {
	return &xds_route.VirtualHost{
		Name: "local_service",
		Domains: []string{
			"*",
		},
		Routes: []*xds_route.Route{
			{
				Match: &xds_route.RouteMatch{
					PathSpecifier: &xds_route.RouteMatch_Prefix{
						Prefix: newPath,
					},
				},
				Action: &xds_route.Route_Route{
					Route: &xds_route.RouteAction{
						ClusterSpecifier: &xds_route.RouteAction_Cluster{
							Cluster: clusterName,
						},
						PrefixRewrite: originalProbePath,
					},
				},
			},
		},
	}
}

// getHTTPAccessLog creates an Envoy AccessLog struct.
func getHTTPAccessLog() (*xds_accesslog_filter.AccessLog, error) {
	accessLog, err := ptypes.MarshalAny(getStdoutAccessLog())
	if err != nil {
		log.Error().Err(err).Str(errcode.Kind, errcode.GetErrCodeWithMetric(errcode.ErrMarshallingXDSResource)).
			Msg("Error marshalling AccessLog object")
		return nil, err
	}
	return &xds_accesslog_filter.AccessLog{
		Name: envoy.AccessLoggerName,
		ConfigType: &xds_accesslog_filter.AccessLog_TypedConfig{
			TypedConfig: accessLog,
		},
	}, nil
}

// getTCPAccessLog creates an Envoy AccessLog struct.
func getTCPAccessLog() (*xds_accesslog_filter.AccessLog, error) {
	accessLog, err := ptypes.MarshalAny(getTCPStdoutAccessLog())
	if err != nil {
		log.Error().Err(err).Str(errcode.Kind, errcode.GetErrCodeWithMetric(errcode.ErrMarshallingXDSResource)).
			Msg("Error marshalling tcp AccessLog object")
		return nil, err
	}
	return &xds_accesslog_filter.AccessLog{
		Name: envoy.AccessLoggerName,
		ConfigType: &xds_accesslog_filter.AccessLog_TypedConfig{
			TypedConfig: accessLog,
		},
	}, nil
}

func getStdoutAccessLog() *xds_accesslog.StdoutAccessLog {
	accessLogger := &xds_accesslog.StdoutAccessLog{
		AccessLogFormat: &xds_accesslog.StdoutAccessLog_LogFormat{
			LogFormat: &xds_core.SubstitutionFormatString{
				Format: &xds_core.SubstitutionFormatString_JsonFormat{
					JsonFormat: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"start_time":            pbStringValue(`%START_TIME%`),
							"method":                pbStringValue(`%REQ(:METHOD)%`),
							"path":                  pbStringValue(`%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%`),
							"protocol":              pbStringValue(`%PROTOCOL%`),
							"response_code":         pbStringValue(`%RESPONSE_CODE%`),
							"response_code_details": pbStringValue(`%RESPONSE_CODE_DETAILS%`),
							"time_to_first_byte":    pbStringValue(`%RESPONSE_DURATION%`),
							"upstream_cluster":      pbStringValue(`%UPSTREAM_CLUSTER%`),
							"response_flags":        pbStringValue(`%RESPONSE_FLAGS%`),
							"bytes_received":        pbStringValue(`%BYTES_RECEIVED%`),
							"bytes_sent":            pbStringValue(`%BYTES_SENT%`),
							"duration":              pbStringValue(`%DURATION%`),
							"upstream_service_time": pbStringValue(`%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%`),
							"x_forwarded_for":       pbStringValue(`%REQ(X-FORWARDED-FOR)%`),
							"user_agent":            pbStringValue(`%REQ(USER-AGENT)%`),
							"request_id":            pbStringValue(`%REQ(X-REQUEST-ID)%`),
							"requested_server_name": pbStringValue("%REQUESTED_SERVER_NAME%"),
							"authority":             pbStringValue(`%REQ(:AUTHORITY)%`),
							"upstream_host":         pbStringValue(`%UPSTREAM_HOST%`),
						},
					},
				},
			},
		},
	}
	return accessLogger
}

func getTCPStdoutAccessLog() *xds_accesslog.StdoutAccessLog {
	accessLogger := &xds_accesslog.StdoutAccessLog{
		AccessLogFormat: &xds_accesslog.StdoutAccessLog_LogFormat{
			LogFormat: &xds_core.SubstitutionFormatString{
				Format: &xds_core.SubstitutionFormatString_JsonFormat{
					JsonFormat: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"start_time":            pbStringValue(`%START_TIME%`),
							"upstream_cluster":      pbStringValue(`%UPSTREAM_CLUSTER%`),
							"response_flags":        pbStringValue(`%RESPONSE_FLAGS%`),
							"bytes_received":        pbStringValue(`%BYTES_RECEIVED%`),
							"bytes_sent":            pbStringValue(`%BYTES_SENT%`),
							"duration":              pbStringValue(`%DURATION%`),
							"requested_server_name": pbStringValue("%REQUESTED_SERVER_NAME%"),
							"upstream_host":         pbStringValue(`%UPSTREAM_HOST%`),
						},
					},
				},
			},
		},
	}
	return accessLogger
}

func pbStringValue(v string) *structpb.Value {
	return &structpb.Value{
		Kind: &structpb.Value_StringValue{
			StringValue: v,
		},
	}
}
