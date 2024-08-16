/*
 *  Copyright (c) 2024, WSO2 LLC. (http://www.wso2.org) All Rights Reserved.
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
 *  This file contains code derived from Envoy Gateway,
 *  https://github.com/envoyproxy/gateway
 *  and is provided here subject to the following:
 *  Copyright Project Envoy Gateway Authors
 *
 */

package translator

import (
	"errors"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	basicauthv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/basic_auth/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/wso2/apk/adapter/internal/operator/gateway-api/ir"
	"github.com/wso2/apk/adapter/internal/types"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	basicAuthFilter = "envoy.filters.http.basic_auth"
)

func init() {
	registerHTTPFilter(&basicAuth{})
}

type basicAuth struct {
}

var _ httpFilter = &basicAuth{}

// patchHCM builds and appends the basic_auth Filters to the HTTP Connection Manager
// if applicable, and it does not already exist.
// Note: this method creates an basic_auth filter for each route that contains an BasicAuth config.
// The filter is disabled by default. It is enabled on the route level.
func (*basicAuth) patchHCM(mgr *hcmv3.HttpConnectionManager, irListener *ir.HTTPListener) error {
	var errs error

	if mgr == nil {
		return errors.New("hcm is nil")
	}

	if irListener == nil {
		return errors.New("ir listener is nil")
	}

	for _, route := range irListener.Routes {
		if !routeContainsBasicAuth(route) {
			continue
		}

		filter, err := buildHCMBasicAuthFilter(route)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		mgr.HttpFilters = append(mgr.HttpFilters, filter)
	}

	return errs
}

// buildHCMBasicAuthFilter returns a basic_auth HTTP filter from the provided IR HTTPRoute.
func buildHCMBasicAuthFilter(route *ir.HTTPRoute) (*hcmv3.HttpFilter, error) {
	basicAuthProto := basicAuthConfig(route)

	if err := basicAuthProto.ValidateAll(); err != nil {
		return nil, err
	}

	basicAuthAny, err := anypb.New(basicAuthProto)
	if err != nil {
		return nil, err
	}

	return &hcmv3.HttpFilter{
		Name:     basicAuthFilterName(route),
		Disabled: true,
		ConfigType: &hcmv3.HttpFilter_TypedConfig{
			TypedConfig: basicAuthAny,
		},
	}, nil
}

func basicAuthFilterName(route *ir.HTTPRoute) string {
	return perRouteFilterName(basicAuthFilter, route.Name)
}

func basicAuthConfig(route *ir.HTTPRoute) *basicauthv3.BasicAuth {
	return &basicauthv3.BasicAuth{
		Users: &corev3.DataSource{
			Specifier: &corev3.DataSource_InlineBytes{
				InlineBytes: route.BasicAuth.Users,
			},
		},
	}
}

// routeContainsBasicAuth returns true if BasicAuth exists for the provided route.
func routeContainsBasicAuth(irRoute *ir.HTTPRoute) bool {
	if irRoute == nil {
		return false
	}

	if irRoute != nil &&
		irRoute.BasicAuth != nil {
		return true
	}

	return false
}

func (*basicAuth) patchResources(*types.ResourceVersionTable, []*ir.HTTPRoute) error {
	return nil
}

// patchRoute patches the provided route with the basicAuth config if applicable.
// Note: this method enables the corresponding basicAuth filter for the provided route.
func (*basicAuth) patchRoute(route *routev3.Route, irRoute *ir.HTTPRoute) error {
	if route == nil {
		return errors.New("xds route is nil")
	}
	if irRoute == nil {
		return errors.New("ir route is nil")
	}
	if irRoute.BasicAuth == nil {
		return nil
	}
	filterName := basicAuthFilterName(irRoute)
	return enableFilterOnRoute(route, filterName)
}
