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

package kube // import "k8s.io/helm/pkg/kube"

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/rest/fake"
	"k8s.io/client-go/restmapper"
	scaleclient "k8s.io/client-go/scale"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/kube-openapi/pkg/util/proto"

	"k8s.io/helm/pkg/kube/factory"
	"k8s.io/helm/pkg/kube/validation"
)

// -----------------------------------------------------------------------------
// FakeCachedDiscoveryClient

type fakeCachedDiscoveryClient struct {
	discovery.DiscoveryInterface
}

func (d *fakeCachedDiscoveryClient) Fresh() bool { return true }

func (d *fakeCachedDiscoveryClient) Invalidate() {}

func (d *fakeCachedDiscoveryClient) ServerResources() ([]*metav1.APIResourceList, error) {
	return []*metav1.APIResourceList{}, nil
}

// -----------------------------------------------------------------------------
// TestFactory

type TestFactory struct {
	factory.Factory

	kubeConfigFlags *genericclioptions.TestConfigFlags

	Client             resource.RESTClient
	ScaleGetter        scaleclient.ScalesGetter
	UnstructuredClient resource.RESTClient
	ClientConfigVal    *restclient.Config
	FakeDynamicClient  *fakedynamic.FakeDynamicClient

	tempConfigFile *os.File

	UnstructuredClientForMappingFunc resource.FakeClientFunc
	OpenAPISchemaFunc                func() (Resources, error)
}

func NewTestFactory() *TestFactory {
	// specify an optionalClientConfig to explicitly use in testing
	// to avoid polluting an existing user config.
	tmpFile, err := ioutil.TempFile("", "cmdtests_temp")
	if err != nil {
		panic(fmt.Sprintf("unable to create a fake client config: %v", err))
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{
		Precedence:     []string{tmpFile.Name()},
		MigrationRules: map[string]string{},
	}

	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmdapi.Cluster{Server: "http://localhost:8080"}}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	configFlags := genericclioptions.NewTestConfigFlags().
		WithClientConfig(clientConfig).
		WithRESTMapper(testRESTMapper())

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		panic(fmt.Sprintf("unable to create a fake restclient config: %v", err))
	}

	return &TestFactory{
		Factory:           factory.New(configFlags),
		kubeConfigFlags:   configFlags,
		FakeDynamicClient: fakedynamic.NewSimpleDynamicClient(scheme.Scheme),
		tempConfigFile:    tmpFile,
		ClientConfigVal:   restConfig,
	}
}

func (f *TestFactory) WithNamespace(ns string) *TestFactory {
	f.kubeConfigFlags.WithNamespace(ns)
	return f
}

func (f *TestFactory) Cleanup() {
	if f.tempConfigFile != nil {
		os.Remove(f.tempConfigFile.Name())
	}
}

func (f *TestFactory) ToRESTConfig() (*restclient.Config, error) {
	return f.ClientConfigVal, nil
}

func (f *TestFactory) ClientForMapping(_ *meta.RESTMapping) (resource.RESTClient, error) {
	return f.Client, nil
}

func (f *TestFactory) UnstructuredClientForMapping(mapping *meta.RESTMapping) (resource.RESTClient, error) {
	if f.UnstructuredClientForMappingFunc != nil {
		return f.UnstructuredClientForMappingFunc(mapping.GroupVersionKind.GroupVersion())
	}
	return f.UnstructuredClient, nil
}

func (f *TestFactory) Validator(validate bool) (validation.Schema, error) {
	return validation.NullSchema{}, nil
}

func (f *TestFactory) OpenAPISchema() (Resources, error) {
	if f.OpenAPISchemaFunc != nil {
		return f.OpenAPISchemaFunc()
	}
	return EmptyResources{}, nil
}

func (f *TestFactory) NewBuilder() *resource.Builder {
	return resource.NewFakeBuilder(
		func(version schema.GroupVersion) (resource.RESTClient, error) {
			if f.UnstructuredClientForMappingFunc != nil {
				return f.UnstructuredClientForMappingFunc(version)
			}
			if f.UnstructuredClient != nil {
				return f.UnstructuredClient, nil
			}
			return f.Client, nil
		},
		f.ToRESTMapper,
		func() (restmapper.CategoryExpander, error) {
			return resource.FakeCategoryExpander, nil
		},
	)
}

func (f *TestFactory) ClientSet() (*kubernetes.Clientset, error) {
	fakeClient := f.Client.(*fake.RESTClient)
	clientset := kubernetes.NewForConfigOrDie(f.ClientConfigVal)

	clientset.CoreV1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.AuthorizationV1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.AuthorizationV1beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.AuthorizationV1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.AuthorizationV1beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.AutoscalingV1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.AutoscalingV2beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.BatchV1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.BatchV2alpha1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.CertificatesV1beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.ExtensionsV1beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.RbacV1alpha1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.RbacV1beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.StorageV1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.StorageV1beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.AppsV1beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.AppsV1beta2().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.PolicyV1beta1().RESTClient().(*restclient.RESTClient).Client = fakeClient.Client
	clientset.DiscoveryClient.RESTClient().(*restclient.RESTClient).Client = fakeClient.Client

	return clientset, nil
}

func (f *TestFactory) DynamicClient() (dynamic.Interface, error) {
	if f.FakeDynamicClient != nil {
		return f.FakeDynamicClient, nil
	}
	return f.Factory.DynamicClient()
}

func (f *TestFactory) RESTClient() (*restclient.RESTClient, error) {
	// Swap out the HTTP client out of the client with the fake's version.
	fakeClient := f.Client.(*fake.RESTClient)
	restClient, err := restclient.RESTClientFor(f.ClientConfigVal)
	if err != nil {
		panic(err)
	}
	restClient.Client = fakeClient.Client
	return restClient, nil
}

func (f *TestFactory) DiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	fakeClient := f.Client.(*fake.RESTClient)

	cacheDir := filepath.Join("", ".kube", "cache", "discovery")
	cachedClient, err := discovery.NewCachedDiscoveryClientForConfig(f.ClientConfigVal, cacheDir, "", 10*time.Minute)
	if err != nil {
		return nil, err
	}
	cachedClient.RESTClient().(*restclient.RESTClient).Client = fakeClient.Client

	return cachedClient, nil
}

func testRESTMapper() meta.RESTMapper {
	groupResources := testDynamicResources()
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	// for backwards compatibility with existing tests, allow rest mappings from the scheme to show up
	// TODO: make this opt-in?
	mapper = meta.FirstHitRESTMapper{
		MultiRESTMapper: meta.MultiRESTMapper{
			mapper,
			testrestmapper.TestOnlyStaticRESTMapper(scheme.Scheme),
		},
	}

	fakeDs := &fakeCachedDiscoveryClient{}
	expander := restmapper.NewShortcutExpander(mapper, fakeDs)
	return expander
}

func (f *TestFactory) ScaleClient() (scaleclient.ScalesGetter, error) {
	return f.ScaleGetter, nil
}

func testDynamicResources() []*restmapper.APIGroupResources {
	return []*restmapper.APIGroupResources{{
		Group: metav1.APIGroup{
			Versions: []metav1.GroupVersionForDiscovery{
				{Version: "v1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1": {
				{Name: "pods", Namespaced: true, Kind: "Pod"},
				{Name: "services", Namespaced: true, Kind: "Service"},
				{Name: "replicationcontrollers", Namespaced: true, Kind: "ReplicationController"},
				{Name: "componentstatuses", Namespaced: false, Kind: "ComponentStatus"},
				{Name: "nodes", Namespaced: false, Kind: "Node"},
				{Name: "secrets", Namespaced: true, Kind: "Secret"},
				{Name: "configmaps", Namespaced: true, Kind: "ConfigMap"},
				{Name: "namespacedtype", Namespaced: true, Kind: "NamespacedType"},
				{Name: "namespaces", Namespaced: false, Kind: "Namespace"},
				{Name: "resourcequotas", Namespaced: true, Kind: "ResourceQuota"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "extensions",
			Versions: []metav1.GroupVersionForDiscovery{
				{Version: "v1beta1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1beta1": {
				{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "apps",
			Versions: []metav1.GroupVersionForDiscovery{
				{Version: "v1beta1"},
				{Version: "v1beta2"},
				{Version: "v1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1beta1": {
				{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
			},
			"v1beta2": {
				{Name: "deployments", Namespaced: true, Kind: "Deployment"},
			},
			"v1": {
				{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "batch",
			Versions: []metav1.GroupVersionForDiscovery{
				{Version: "v1beta1"},
				{Version: "v1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1beta1": {
				{Name: "cronjobs", Namespaced: true, Kind: "CronJob"},
			},
			"v1": {
				{Name: "jobs", Namespaced: true, Kind: "Job"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "autoscaling",
			Versions: []metav1.GroupVersionForDiscovery{
				{Version: "v1"},
				{Version: "v2beta1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v2beta1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1": {
				{Name: "horizontalpodautoscalers", Namespaced: true, Kind: "HorizontalPodAutoscaler"},
			},
			"v2beta1": {
				{Name: "horizontalpodautoscalers", Namespaced: true, Kind: "HorizontalPodAutoscaler"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "storage.k8s.io",
			Versions: []metav1.GroupVersionForDiscovery{
				{Version: "v1beta1"},
				{Version: "v0"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1beta1": {
				{Name: "storageclasses", Namespaced: false, Kind: "StorageClass"},
			},
			// bogus version of a known group/version/resource to make sure kubectl falls back to generic object mode
			"v0": {
				{Name: "storageclasses", Namespaced: false, Kind: "StorageClass"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "rbac.authorization.k8s.io",
			Versions: []metav1.GroupVersionForDiscovery{
				{Version: "v1beta1"},
				{Version: "v1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1": {
				{Name: "clusterroles", Namespaced: false, Kind: "ClusterRole"},
			},
			"v1beta1": {
				{Name: "clusterrolebindings", Namespaced: false, Kind: "ClusterRoleBinding"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "company.com",
			Versions: []metav1.GroupVersionForDiscovery{
				{Version: "v1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1": {
				{Name: "bars", Namespaced: true, Kind: "Bar"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "unit-test.test.com",
			Versions: []metav1.GroupVersionForDiscovery{
				{GroupVersion: "unit-test.test.com/v1", Version: "v1"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{
				GroupVersion: "unit-test.test.com/v1",
				Version:      "v1"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"v1": {
				{Name: "widgets", Namespaced: true, Kind: "Widget"},
			},
		},
	}, {
		Group: metav1.APIGroup{
			Name: "apitest",
			Versions: []metav1.GroupVersionForDiscovery{
				{GroupVersion: "apitest/unlikelyversion", Version: "unlikelyversion"},
			},
			PreferredVersion: metav1.GroupVersionForDiscovery{
				GroupVersion: "apitest/unlikelyversion",
				Version:      "unlikelyversion"},
		},
		VersionedResources: map[string][]metav1.APIResource{
			"unlikelyversion": {
				{Name: "types", SingularName: "type", Namespaced: false, Kind: "Type"},
			},
		},
	}}
}

// -----------------------------------------------------------------------------
// OpenAPI

// Resources interface describe a resources provider, that can give you
// resource based on group-version-kind.
type Resources interface {
	LookupResource(gvk schema.GroupVersionKind) proto.Schema
}

// EmptyResources implement a Resources that just doesn't have any resources.
type EmptyResources struct{}

// LookupResource will always return nil. It doesn't have any resources.
func (EmptyResources) LookupResource(_ schema.GroupVersionKind) proto.Schema { return nil }
