/*
 *  Copyright (c) 2022, WSO2 LLC. (http://www.wso2.org) All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *
 */
package envoyconf_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wso2/apk/adapter/config"
	envoy "github.com/wso2/apk/adapter/internal/oasparser/envoyconf"
	"github.com/wso2/apk/adapter/internal/oasparser/model"
	"github.com/wso2/apk/adapter/internal/oasparser/utils"
	"github.com/wso2/apk/adapter/internal/operator/apis/dp/v1alpha1"
	dpv1alpha1 "github.com/wso2/apk/adapter/internal/operator/apis/dp/v1alpha1"
	"github.com/wso2/apk/adapter/internal/operator/synchronizer"
	operatorutils "github.com/wso2/apk/adapter/internal/operator/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestCreateRoutesWithClusters(t *testing.T) {

	var MockHostNameResolver = func(client ctrlclient.Client, backend gwapiv1b1.HTTPBackendRef,
		defaultNamespace string) string {
		return fmt.Sprintf("%s.%s", backend.Name, "default")
	}

	apiState := synchronizer.APIState{}
	apiDefinition := v1alpha1.API{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-api-1",
		},
		Spec: v1alpha1.APISpec{
			APIDisplayName:   "test-api",
			APIVersion:       "1.0.0",
			Context:          "/test-api/1.0.0",
			ProdHTTPRouteRef: "test-api-1-prod-http-route",
		},
	}
	apiState.APIDefinition = &apiDefinition
	httpRouteState := synchronizer.HTTPRouteState{}

	httpRoute := gwapiv1b1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-api-1-prod-http-route",
		},
		Spec: gwapiv1b1.HTTPRouteSpec{
			Hostnames:       []gwapiv1b1.Hostname{"prod.gw.wso2.com"},
			CommonRouteSpec: createDefaultCommonRouteSpec(),
			Rules: []gwapiv1b1.HTTPRouteRule{
				{
					Matches: []gwapiv1b1.HTTPRouteMatch{
						{
							Path: &gwapiv1b1.HTTPPathMatch{
								Type:  operatorutils.PathMatchTypePtr(gwapiv1b1.PathMatchPathPrefix),
								Value: operatorutils.StringPtr("/test-api/1.0.0/orders"),
							},
						},
					},
					Filters: []gwapiv1b1.HTTPRouteFilter{
						{
							Type: gwapiv1b1.HTTPRouteFilterType("URLRewrite"),
							URLRewrite: &gwapiv1b1.HTTPURLRewriteFilter{
								Path: &gwapiv1b1.HTTPPathModifier{
									Type:               gwapiv1b1.PrefixMatchHTTPPathModifier,
									ReplacePrefixMatch: operatorutils.StringPtr("/backend-base-path/orders"),
								},
							},
						},
					},
					BackendRefs: []gwapiv1b1.HTTPBackendRef{
						createDefaultBackendRef("order-service", 80, 1),
						createDefaultBackendRef("order-service-2", 8080, 1),
					},
				},
				{
					Matches: []gwapiv1b1.HTTPRouteMatch{
						{
							Path: &gwapiv1b1.HTTPPathMatch{
								Type:  operatorutils.PathMatchTypePtr(gwapiv1b1.PathMatchPathPrefix),
								Value: operatorutils.StringPtr("/test-api/1.0.0/users"),
							},
						},
					},
					Filters: []gwapiv1b1.HTTPRouteFilter{
						{
							Type: gwapiv1b1.HTTPRouteFilterType("URLRewrite"),
							URLRewrite: &gwapiv1b1.HTTPURLRewriteFilter{
								Path: &gwapiv1b1.HTTPPathModifier{
									Type:               gwapiv1b1.PrefixMatchHTTPPathModifier,
									ReplacePrefixMatch: operatorutils.StringPtr("/backend-base-path/users"),
								},
							},
						},
					},
					BackendRefs: []gwapiv1b1.HTTPBackendRef{
						createDefaultBackendRef("user-service", 8081, 1),
						createDefaultBackendRef("user-service-2", 8081, 1),
					},
				},
			},
		},
	}

	httpRouteState.HTTPRoute = &httpRoute
	httpRouteState.Authentications = make(map[string]dpv1alpha1.Authentication)
	httpRouteState.ResourceAuthentications = make(map[string]dpv1alpha1.Authentication)

	apiState.ProdHTTPRoute = &httpRouteState

	mgwSwagger, err := synchronizer.GenerateMGWSwagger(apiState, &httpRouteState, true, MockHostNameResolver)
	assert.Nil(t, err, "Error should not be present when apiState is converted to a MgwSwagger object")
	routes, clusters, _, _ := envoy.CreateRoutesWithClusters(*mgwSwagger, nil, nil, "prod.gw.wso2.com", "carbon.super")
	assert.Equal(t, 2, len(clusters), "Number of production clusters created is incorrect.")

	orderServiceCluster := clusters[0]

	assert.Equal(t, orderServiceCluster.GetName(), "carbon.super_clusterProd_prod.gw.wso2.com_test-api1.0.0", "Path Level cluster name mismatch, %v", clusters)
	assert.Equal(t, orderServiceCluster.GetName(), "carbon.super_clusterProd_prod.gw.wso2.com_test-api1.0.0", "Path Level cluster name mismatch, %v", clusters)

	orderServiceClusterHost0 := orderServiceCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetAddress()
	orderServiceClusterPort0 := orderServiceCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetPortValue()
	orderServiceClusterPriority0 := orderServiceCluster.GetLoadAssignment().GetEndpoints()[0].Priority
	orderServiceClusterHost1 := orderServiceCluster.GetLoadAssignment().GetEndpoints()[1].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetAddress()
	orderServiceClusterPort1 := orderServiceCluster.GetLoadAssignment().GetEndpoints()[1].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetPortValue()
	orderServiceClusterPriority1 := orderServiceCluster.GetLoadAssignment().GetEndpoints()[1].Priority

	assert.NotEmpty(t, orderServiceClusterHost0, "Order Service Cluster's assigned host should not be null")
	assert.Equal(t, "order-service.default", orderServiceClusterHost0, "Order Service Cluster's assigned host is incorrect.")
	assert.NotEmpty(t, orderServiceClusterPort0, "Order Service Cluster's assigned port should not be null")
	assert.Equal(t, uint32(80), orderServiceClusterPort0, "Order Service Cluster's assigned port is incorrect.")
	assert.Equal(t, uint32(0), orderServiceClusterPriority0, "Order Service Cluster's assigned Priority is incorrect.")

	assert.NotEmpty(t, orderServiceClusterHost1, "Order Service Cluster's second endpoint host should not be null")
	assert.Equal(t, "order-service-2.default", orderServiceClusterHost1, "Order Service Cluster's second endpoint host is incorrect.")
	assert.NotEmpty(t, orderServiceClusterPort1, "Order Service Cluster's second endpoint port should not be null")
	assert.Equal(t, uint32(8080), orderServiceClusterPort1, "Order Service Cluster's second endpoint port is incorrect.")
	assert.Equal(t, uint32(0), orderServiceClusterPriority1, "Order Service Cluster's second endpoint Priority is incorrect.")

	userServiceCluster := clusters[1]

	userServiceClusterHost0 := userServiceCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetAddress()
	userServiceClusterPort0 := userServiceCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetPortValue()
	userServiceClusterPriority0 := userServiceCluster.GetLoadAssignment().GetEndpoints()[0].Priority
	userServiceClusterHost1 := userServiceCluster.GetLoadAssignment().GetEndpoints()[1].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetAddress()
	userServiceClusterPort1 := userServiceCluster.GetLoadAssignment().GetEndpoints()[1].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetPortValue()
	userServiceClusterPriority1 := userServiceCluster.GetLoadAssignment().GetEndpoints()[1].Priority

	assert.NotEmpty(t, userServiceClusterHost0, "User Service Cluster's assigned host should not be null")
	assert.Equal(t, "user-service.default", userServiceClusterHost0, "User Service Cluster's assigned host is incorrect.")
	assert.NotEmpty(t, userServiceClusterPort0, "User Service Cluster's assigned port should not be null")
	assert.Equal(t, uint32(8081), userServiceClusterPort0, "User Service Cluster's assigned host is incorrect.")
	assert.Equal(t, uint32(0), userServiceClusterPriority0, "User Service Cluster's assigned priority is incorrect.")

	assert.NotEmpty(t, userServiceClusterHost1, "User Service Cluster's second endpoint host should not be null")
	assert.Equal(t, "user-service-2.default", userServiceClusterHost1, "User Service Cluster's second endpoint host is incorrect.")
	assert.NotEmpty(t, userServiceClusterPort1, "User Service Cluster's second endpoint port should not be null")
	assert.Equal(t, uint32(8081), userServiceClusterPort1, "User Service Cluster's second endpoint port is incorrect.")
	assert.Equal(t, uint32(0), userServiceClusterPriority1, "API Level Cluster's second endpoint Priority is incorrect.")

	assert.Equal(t, 14, len(routes), "Created number of routes are incorrect.")
	assert.Contains(t, []string{"^/test-api/1.0.0/orders(/.*)*"}, routes[0].GetMatch().GetSafeRegex().Regex)
	assert.Contains(t, []string{"^/test-api/1.0.0/users(/.*)*"}, routes[7].GetMatch().GetSafeRegex().Regex)
	assert.NotEqual(t, routes[0].GetMatch().GetSafeRegex().Regex, routes[7].GetMatch().GetSafeRegex().Regex,
		"The route regex for the two paths should not be the same")
}

func createDefaultCommonRouteSpec() gwapiv1b1.CommonRouteSpec {
	return gwapiv1b1.CommonRouteSpec{
		ParentRefs: []gwapiv1b1.ParentReference{
			{
				Group: operatorutils.GroupPtr("gateway.networking.k8s.io"),
				Kind:  operatorutils.KindPtr("Gateway"),
				Name:  gwapiv1b1.ObjectName("default-gateway"),
			},
		},
	}
}

func createDefaultBackendRef(serviceName string, port int32, weight int32) gwapiv1b1.HTTPBackendRef {
	backendPort := gwapiv1b1.PortNumber(port)
	return gwapiv1b1.HTTPBackendRef{
		BackendRef: gwapiv1b1.BackendRef{
			BackendObjectReference: gwapiv1b1.BackendObjectReference{
				Group: operatorutils.GroupPtr(""),
				Kind:  operatorutils.KindPtr("Service"),
				Name:  gwapiv1b1.ObjectName(serviceName),
				Port:  &backendPort,
			},
			Weight: &weight,
		},
	}
}

func testCreateRoutesWithClustersWebsocket(t *testing.T, apiYamlFilePath string) {
	// If the asyncAPI definition contains the production and sandbox endpoints, they are prioritized over
	// the api.yaml. If the asyncAPI definition does not have any of them, api.yaml's value is assigned.
	apiYamlByteArr, err := ioutil.ReadFile(apiYamlFilePath)
	assert.Nil(t, err, "Error while reading the api.yaml file : %v"+apiYamlFilePath)
	apiYaml, err := model.NewAPIYaml(apiYamlByteArr)
	assert.Nil(t, err, "Error occurred while processing api.yaml")
	var mgwSwagger model.MgwSwagger
	err = mgwSwagger.PopulateFromAPIYaml(apiYaml)

	asyncapiFilePath := config.GetMgwHome() + "/../adapter/test-resources/envoycodegen/asyncapi_websocket.yaml"
	asyncapiByteArr, err := ioutil.ReadFile(asyncapiFilePath)
	assert.Nil(t, err, "Error while reading file : %v"+asyncapiFilePath)
	apiJsn, conversionErr := utils.ToJSON(asyncapiByteArr)
	assert.Nil(t, conversionErr, "YAML to JSON conversion error : %v"+asyncapiFilePath)

	var asyncapi model.AsyncAPI
	err = json.Unmarshal(apiJsn, &asyncapi)
	assert.Nil(t, err, "Error occurred while parsing asyncapi_websocket.yaml")

	err = mgwSwagger.SetInfoAsyncAPI(asyncapi)
	assert.Nil(t, err, "Error while populating the MgwSwagger object for web socket APIs")
	routes, clusters, _, _ := envoy.CreateRoutesWithClusters(mgwSwagger, nil, nil, "localhost", "carbon.super")

	if strings.HasSuffix(apiYamlFilePath, "api.yaml") {
		assert.Equal(t, len(clusters), 2, "Number of clusters created incorrect")
		productionCluster := clusters[0]
		sandBoxCluster := clusters[1]
		assert.Equal(t, productionCluster.GetName(), "carbon.super_clusterProd_localhost_EchoWebSocket1.0", "Production cluster name mismatch")
		assert.Equal(t, sandBoxCluster.GetName(), "carbon.super_clusterSand_localhost_EchoWebSocket1.0", "Sandbox cluster name mismatch")

		productionClusterHost := productionCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
		productionClusterPort := productionCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()

		assert.Equal(t, productionClusterHost, "ws.ifelse.io", "Production cluster host mismatch")
		assert.Equal(t, productionClusterPort, uint32(443), "Production cluster port mismatch")

		sandBoxClusterHost := sandBoxCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
		sandBoxClusterPort := sandBoxCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()

		assert.Equal(t, sandBoxClusterHost, "echo.websocket.org", "Sandbox cluster host mismatch")
		assert.Equal(t, sandBoxClusterPort, uint32(80), "Sandbox cluster port mismatch")

		assert.Equal(t, 2, len(routes), "Number of routes incorrect")

		route := routes[0].GetMatch().GetSafeRegex().Regex
		assert.Equal(t, "^/echowebsocket/1.0/notifications[/]{0,1}", route, "route created mismatch")

		throttlingPolicy := mgwSwagger.GetXWso2ThrottlingTier()
		assert.Equal(t, throttlingPolicy, "5PerMin", "API throttling policy is not assigned.")
	}
	if strings.HasSuffix(apiYamlFilePath, "api_prod.yaml") {
		assert.Equal(t, len(clusters), 1, "Number of clusters created incorrect")
		productionCluster := clusters[0]
		assert.Equal(t, productionCluster.GetName(), "carbon.super_clusterProd_localhost_prodws1.0", "Production cluster name mismatch")

		productionClusterHost := productionCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
		productionClusterPort := productionCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()

		assert.Equal(t, productionClusterHost, "ws.ifelse.io", "Production cluster host mismatch")
		assert.Equal(t, productionClusterPort, uint32(443), "Production cluster port mismatch")

		assert.Equal(t, 2, len(routes), "Number of routes incorrect")

		route := routes[0].GetMatch().GetSafeRegex().Regex
		assert.Equal(t, route, "^/echowebsocketprod/1.0/notifications[/]{0,1}", "route created mismatch")

		// TODO: (VirajSalaka) add Unit test for second resource too.
		route2 := routes[1].GetMatch().GetSafeRegex().Regex
		assert.Equal(t, route2, "^/echowebsocketprod/1.0/rooms/([^/]+)[/]{0,1}", "route created mismatch")

	}
	if strings.HasSuffix(apiYamlFilePath, "api_sand.yaml") {
		assert.Equal(t, len(clusters), 2, "Number of clusters created incorrect")
		sandBoxCluster := clusters[1]
		assert.Equal(t, sandBoxCluster.GetName(), "carbon.super_clusterSand_localhost_sandbox1.0", "Sandbox cluster name mismatch")

		sandBoxClusterHost := sandBoxCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
		sandBoxClusterPort := sandBoxCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()

		assert.Equal(t, sandBoxClusterHost, "echo.websocket.org", "Production cluster host mismatch")
		assert.Equal(t, sandBoxClusterPort, uint32(80), "Production cluster port mismatch")

	}

}

func TestCreateHealthEndpoint(t *testing.T) {
	route := envoy.CreateHealthEndpoint()
	assert.NotNil(t, route, "Health Endpoint Route should not be null.")
	assert.Equal(t, "/health", route.Name, "Health Route Name is incorrect.")
	assert.Equal(t, "/health", route.GetMatch().GetPath(), "Health route path is incorrect.")
	assert.Equal(t, "{\"status\": \"healthy\"}", route.GetDirectResponse().GetBody().GetInlineString(), "Health response message is incorrect.")
	assert.Equal(t, uint32(200), route.GetDirectResponse().GetStatus(), "Health response status is incorrect.")
}

// commonTestForClusterPriorities use to test loadbalance/failover in WS apis
func commonTestForClusterPrioritiesInWebSocketAPI(t *testing.T, apiYamlFilePath string) {
	apiYamlByteArr, err := ioutil.ReadFile(apiYamlFilePath)
	assert.Nil(t, err, "Error while reading the api.yaml file : %v"+apiYamlFilePath)
	apiYaml, err := model.NewAPIYaml(apiYamlByteArr)
	assert.Nil(t, err, "Error occurred while processing api.yaml")
	var mgwSwagger model.MgwSwagger
	err = mgwSwagger.PopulateFromAPIYaml(apiYaml)
	assert.Nil(t, err, "Error while populating the MgwSwagger object for web socket APIs")
	_, clusters, _, _ := envoy.CreateRoutesWithClusters(mgwSwagger, nil, nil, "localhost", "carbon.super")

	assert.Equal(t, len(clusters), 1, "Number of clusters created incorrect")
	productionCluster := clusters[0]
	sandBoxCluster := clusters[0]

	productionClusterHost0 := productionCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
	productionClusterPort0 := productionCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()
	productionClusterPriority0 := productionCluster.GetLoadAssignment().GetEndpoints()[0].Priority
	productionClusterHost1 := productionCluster.GetLoadAssignment().GetEndpoints()[1].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
	productionClusterPort1 := productionCluster.GetLoadAssignment().GetEndpoints()[1].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()
	productionClusterPriority1 := productionCluster.GetLoadAssignment().GetEndpoints()[1].Priority

	sandBoxClusterHost0 := sandBoxCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
	sandBoxClusterPort0 := sandBoxCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()
	sandBoxClusterPriority0 := sandBoxCluster.GetLoadAssignment().GetEndpoints()[0].Priority
	sandBoxClusterHost1 := sandBoxCluster.GetLoadAssignment().GetEndpoints()[1].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
	sandBoxClusterPort1 := sandBoxCluster.GetLoadAssignment().GetEndpoints()[1].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetPortValue()
	sandBoxClusterPriority1 := sandBoxCluster.GetLoadAssignment().GetEndpoints()[1].Priority

	assert.Equal(t, "primary.websocket.org", productionClusterHost0, "Production endpoint host mismatch")
	assert.Equal(t, uint32(443), productionClusterPort0, "Production endpoint port mismatch")
	assert.Equal(t, uint32(0), productionClusterPriority0, "Production endpoint priority mismatch")

	assert.Equal(t, "echo.websocket.org", productionClusterHost1, "Second production endpoint host mismatch")
	assert.Equal(t, uint32(80), productionClusterPort1, "Second production endpoint port mismatch")

	assert.Equal(t, sandBoxClusterHost0, "primary.websocket.org", "Sandbox cluster host mismatch")
	assert.Equal(t, sandBoxClusterPort0, uint32(443), "Sandbox cluster port mismatch")
	assert.Equal(t, uint32(0), sandBoxClusterPriority0, "Sandbox endpoint priority mismatch")

	assert.Equal(t, sandBoxClusterHost1, "echo.websocket.org", "Sandbox cluster host mismatch")
	assert.Equal(t, sandBoxClusterPort1, uint32(80), "Second sandbox cluster port mismatch")

	if strings.HasSuffix(apiYamlFilePath, "ws_api_loadbalance.yaml") {
		assert.Equal(t, uint32(0), productionClusterPriority1, "Second production endpoint port mismatch")
		assert.Equal(t, uint32(0), sandBoxClusterPriority1, "Second sandbox endpoint priority mismatch")
	}

	if strings.HasSuffix(apiYamlFilePath, "ws_api_failover.yaml") {
		assert.Equal(t, uint32(1), productionClusterPriority1, "Second production endpoint port mismatch")
		assert.Equal(t, uint32(1), sandBoxClusterPriority1, "Second sandbox endpoint priority mismatch")
	}
}

func testCreateRoutesWithClustersAPIClusters(t *testing.T) {
	openapiFilePath := config.GetMgwHome() + "/../adapter/test-resources/envoycodegen/openapi_prod_sand_clusters.yaml"
	openapiByteArr, err := ioutil.ReadFile(openapiFilePath)
	assert.Nil(t, err, "Error while reading the openapi file : "+openapiFilePath)
	mgwSwaggerForOpenapi := model.MgwSwagger{}
	err = mgwSwaggerForOpenapi.GetMgwSwagger(openapiByteArr)
	assert.Nil(t, err, "Error should not be present when openAPI definition is converted to a MgwSwagger object")
	routes, clusters, _, _ := envoy.CreateRoutesWithClusters(mgwSwaggerForOpenapi, nil, nil, "localhost", "carbon.super")

	assert.Equal(t, 2, len(clusters), "Number of production clusters created is incorrect.")
	// As the first cluster is always related to API level cluster
	apiLevelCluster := clusters[0]
	assert.Equal(t, apiLevelCluster.GetName(), "carbon.super_clusterProd_localhost_SwaggerPetstore1.0.0", "API Level cluster name mismatch")

	apiLevelClusterHost0 := apiLevelCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetAddress()
	apiLevelClusterPort0 := apiLevelCluster.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetPortValue()
	apiLevelClusterPriority0 := apiLevelCluster.GetLoadAssignment().GetEndpoints()[0].Priority

	assert.NotEmpty(t, apiLevelClusterHost0, "API Level Cluster's assigned host should not be null")
	assert.Equal(t, "apiLevelProdEndpoint", apiLevelClusterHost0, "API Level Cluster's assigned host is incorrect.")
	assert.NotEmpty(t, apiLevelClusterPort0, "API Level Cluster's assigned port should not be null")
	assert.Equal(t, uint32(80), apiLevelClusterPort0, "API Level Cluster's assigned host is incorrect.")
	assert.Equal(t, uint32(0), apiLevelClusterPriority0, "API Level Cluster's assigned Priority is incorrect.")

	resourceLevelCluster0 := clusters[1]
	assert.Contains(t, resourceLevelCluster0.GetName(), "carbon.super_clusterProd_localhost_SwaggerPetstore1.0.0_", "Resource Level cluster name mismatch")

	resourceLevelClusterHost0 := resourceLevelCluster0.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetAddress()
	resourceLevelClusterPort0 := resourceLevelCluster0.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().
		GetAddress().GetSocketAddress().GetPortValue()
	resourceLevelClusterPriority0 := resourceLevelCluster0.GetLoadAssignment().GetEndpoints()[0].Priority

	assert.NotEmpty(t, resourceLevelClusterHost0, "API Level Cluster's assigned host should not be null")
	assert.Equal(t, "resourceLevelProdEndpoint", resourceLevelClusterHost0, "API Level Cluster's assigned host is incorrect.")
	assert.Equal(t, uint32(443), resourceLevelClusterPort0, "API Level Cluster's assigned host is incorrect.")
	assert.Equal(t, uint32(0), resourceLevelClusterPriority0, "API Level Cluster's assigned Priority is incorrect.")

	assert.Equal(t, 2, len(routes), "Number of routes created is incorrect")
}
