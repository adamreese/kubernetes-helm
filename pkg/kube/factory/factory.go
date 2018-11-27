/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package factory // import "k8s.io/helm/pkg/kube/factory"

import (
	"sync"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"

	"k8s.io/helm/pkg/kube/openapi"
	openapivalidation "k8s.io/helm/pkg/kube/openapi/validation"
	"k8s.io/helm/pkg/kube/validation"
)

type factoryImpl struct {
	ClientGetter

	// openAPIGetter loads and caches openapi specs
	openAPIGetter struct {
		sync.Once
		openapi.Getter
	}
}

func New(getter genericclioptions.RESTClientGetter) Factory {
	if getter == nil {
		panic("attempt to instantiate client_access_factory with nil clientGetter")
	}

	return &factoryImpl{
		ClientGetter: newClientGetter(getter),
	}
}

// NewBuilder returns a new resource builder for structured api objects.
func (f *factoryImpl) NewBuilder() *resource.Builder {
	return resource.NewBuilder(f)
}

// Validator returns a schema that can validate objects.
func (f *factoryImpl) Validator(validate bool) (validation.Schema, error) {
	if !validate {
		return validation.NullSchema{}, nil
	}

	resources, err := f.openAPISchema()
	if err != nil {
		return nil, err
	}

	return validation.ConjunctiveSchema{
		openapivalidation.NewSchemaValidation(resources),
		validation.NoDoubleKeySchema{},
	}, nil
}

// openAPISchema returns metadata and structural information about Kubernetes object definitions.
func (f *factoryImpl) openAPISchema() (openapi.Resources, error) {
	discovery, err := f.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}

	// Lazily initialize the OpenAPIGetter once
	f.openAPIGetter.Do(func() {
		// Create the caching OpenAPIGetter
		f.openAPIGetter.Getter = openapi.NewOpenAPIGetter(discovery)
	})

	// Delegate to the OpenAPIGetter
	return f.openAPIGetter.Get()
}
