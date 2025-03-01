package eds

import (
	"fmt"
	"testing"

	xds_endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	xds_discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/mock/gomock"
	tassert "github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes"
	testclient "k8s.io/client-go/kubernetes/fake"

	configFake "github.com/openservicemesh/osm/pkg/gen/client/config/clientset/versioned/fake"
	"github.com/openservicemesh/osm/pkg/service"

	"github.com/openservicemesh/osm/pkg/catalog"
	"github.com/openservicemesh/osm/pkg/certificate"
	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/constants"
	"github.com/openservicemesh/osm/pkg/envoy"
	"github.com/openservicemesh/osm/pkg/tests"
)

func getProxy(kubeClient kubernetes.Interface) (*envoy.Proxy, error) {
	podLabels := map[string]string{
		tests.SelectorKey:                tests.BookbuyerService.Name,
		constants.EnvoyUniqueIDLabelName: tests.ProxyUUID,
	}
	if _, err := tests.MakePod(kubeClient, tests.Namespace, tests.BookbuyerServiceName, tests.BookbuyerServiceAccountName, podLabels); err != nil {
		return nil, err
	}

	selectors := map[string]string{
		tests.SelectorKey: tests.BookbuyerServiceName,
	}
	if _, err := tests.MakeService(kubeClient, tests.BookbuyerServiceName, selectors); err != nil {
		return nil, err
	}

	for _, svcName := range []string{tests.BookstoreApexServiceName, tests.BookstoreV1ServiceName, tests.BookstoreV2ServiceName} {
		selectors := map[string]string{
			tests.SelectorKey: "bookstore",
		}
		if _, err := tests.MakeService(kubeClient, svcName, selectors); err != nil {
			return nil, err
		}
	}

	certCommonName := certificate.CommonName(fmt.Sprintf("%s.%s.%s.%s", tests.ProxyUUID, envoy.KindSidecar, tests.BookbuyerServiceAccountName, tests.Namespace))
	certSerialNumber := certificate.SerialNumber("123456")
	return envoy.NewProxy(certCommonName, certSerialNumber, nil)
}

func TestEndpointConfiguration(t *testing.T) {
	assert := tassert.New(t)
	mockCtrl := gomock.NewController(t)
	mockConfigurator := configurator.NewMockConfigurator(mockCtrl)
	kubeClient := testclient.NewSimpleClientset()
	configClient := configFake.NewSimpleClientset()

	meshCatalog := catalog.NewFakeMeshCatalog(kubeClient, configClient)

	proxy, err := getProxy(kubeClient)
	assert.Empty(err)
	assert.NotNil(meshCatalog)
	assert.NotNil(proxy)

	request := &xds_discovery.DiscoveryRequest{
		ResourceNames: []string{"default/bookstore-v1"},
	}
	resources, err := NewResponse(meshCatalog, proxy, request, mockConfigurator, nil, nil)
	assert.Nil(err)
	assert.NotNil(resources)

	// There are 3 endpoints configured based on the configuration:
	// 1. Bookstore
	// 2. Bookstore-v1
	// 3. Bookstore-v2
	assert.Len(resources, 1)

	loadAssignment, ok := resources[0].(*xds_endpoint.ClusterLoadAssignment)

	// validating an endpoint
	assert.True(ok)
	assert.Len(loadAssignment.Endpoints, 1)
}

func TestClusterToMeshSvc(t *testing.T) {
	testCases := []struct {
		name            string
		cluster         string
		expectedMeshSvc service.MeshService
		expectError     bool
	}{
		{
			name:            "invalid cluster name",
			cluster:         "foo/bar/local",
			expectedMeshSvc: service.MeshService{},
			expectError:     true,
		},
		{
			name:            "invalid cluster name",
			cluster:         "foobar",
			expectedMeshSvc: service.MeshService{},
			expectError:     true,
		},
		{
			name:    "valid cluster name",
			cluster: "foo/bar",
			expectedMeshSvc: service.MeshService{
				Namespace: "foo",
				Name:      "bar",
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			meshSvc, err := clusterToMeshSvc(tc.cluster)
			assert.Equal(tc.expectError, err != nil)
			assert.Equal(tc.expectedMeshSvc, meshSvc)
		})
	}
}
