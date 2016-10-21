/*
Copyright 2016 The Kubernetes Authors All rights reserved.

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

package kube

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/meta"
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/apis/extensions"
	"k8s.io/kubernetes/pkg/client/typed/dynamic"
	"k8s.io/kubernetes/pkg/client/unversioned/fake"
	"k8s.io/kubernetes/pkg/kubectl/resource"
	"k8s.io/kubernetes/pkg/runtime"
)

func defaultHeader() http.Header {
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)
	return header
}

func TestUpdateFull(t *testing.T) {
	labels := map[string]string{"app": "foo"}

	deployment := extensions.Deployment{
		ObjectMeta: api.ObjectMeta{
			Name:   "abc",
			Labels: labels,
		},
		Spec: extensions.DeploymentSpec{
			Replicas: 1,
			Selector: &unversioned.LabelSelector{MatchLabels: labels},
			Template: api.PodTemplateSpec{
				ObjectMeta: api.ObjectMeta{
					Labels: labels,
				},
				Spec: api.PodSpec{
					Containers: []api.Container{{Name: "app-v4", Image: "abc/app:v4"}},
				},
			},
		},
	}

	original, err := runtime.Encode(testapi.Extensions.Codec(), &deployment)
	if err != nil {
		t.Fatal(err)
	}
	b := bytes.NewBuffer(original)

	deployment.Spec.Template.Spec.Containers[0].Image = "abc/app:v5"
	modified, err := runtime.Encode(testapi.Extensions.Codec(), &deployment)
	if err != nil {
		t.Fatal(err)
	}
	b2 := bytes.NewBuffer(modified)

	c := New(nil)
	c.ClientForMapping = func(mapping *meta.RESTMapping) (resource.RESTClient, error) {
		return &fake.RESTClient{
			NegotiatedSerializer: dynamic.ContentConfig().NegotiatedSerializer,
			Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
				switch p, m := req.URL.Path, req.Method; {
				case p == "/namespaces/default/deployments/abc" && m == http.MethodGet:
					return &http.Response{StatusCode: http.StatusCreated, Header: defaultHeader(), Body: ioutil.NopCloser(bytes.NewBuffer(original))}, nil
				case p == "/namespaces/default/deployments/abc" && m == http.MethodPatch:
					return &http.Response{StatusCode: http.StatusCreated, Header: defaultHeader(), Body: ioutil.NopCloser(bytes.NewBuffer(modified))}, nil
				default:
					t.Fatalf("unexpected request: %#v\n%#v", req.URL, req)
					return nil, nil
				}
			}),
		}, nil
	}

	if err := c.Update(api.NamespaceDefault, b, b2); err != nil {
		t.Errorf("Expected success, got failure on update: %v", err)
	}
}

func TestUpdateResource(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		modified   *resource.Info
		currentObj runtime.Object
		err        bool
		errMessage string
	}{
		{
			name:       "no changes when updating resources",
			modified:   createFakeInfo("nginx", nil),
			currentObj: createFakePod("nginx", nil),
			err:        true,
			errMessage: "Looks like there are no changes for nginx",
		},
		//{
		//name:       "valid update input",
		//modified:   createFakeInfo("nginx", map[string]string{"app": "nginx"}),
		//currentObj: createFakePod("nginx", nil),
		//},
	}

	for _, tt := range tests {
		err := updateResource(tt.modified, tt.currentObj)
		if err != nil && err.Error() != tt.errMessage {
			t.Errorf("%q. expected error message: %v, got %v", tt.name, tt.errMessage, err)
		}
	}
}

func TestPerform(t *testing.T) {
	guestbook, err := os.Open("testdata/guestbook.yaml")
	if err != nil {
		t.Fatalf("could not read ./testdata/guestbook.yaml: %v", err)
	}
	defer guestbook.Close()

	tests := []struct {
		name       string
		namespace  string
		reader     io.Reader
		count      int
		err        bool
		errMessage string
	}{
		{
			name:      "Valid input",
			namespace: "test",
			reader:    guestbook,
			count:     6,
		}, {
			name:       "Empty manifests",
			namespace:  "test",
			reader:     strings.NewReader(""),
			err:        true,
			errMessage: "no objects visited",
		},
	}

	for _, tt := range tests {
		results := []*resource.Info{}

		fn := func(info *resource.Info) error {
			results = append(results, info)

			if info.Namespace != tt.namespace {
				t.Errorf("%q. expected namespace to be '%s', got %s", tt.name, tt.namespace, info.Namespace)
			}
			return nil
		}

		c := New(nil)
		c.IncludeThirdPartyAPIs = false
		c.ClientForMapping = func(mapping *meta.RESTMapping) (resource.RESTClient, error) {
			return &fake.RESTClient{}, nil
		}

		err := perform(c, tt.namespace, tt.reader, fn)
		if (err != nil) != tt.err {
			t.Errorf("%q. expected error: %v, got %v", tt.name, tt.err, err)
		}
		if err != nil && err.Error() != tt.errMessage {
			t.Errorf("%q. expected error message: %v, got %v", tt.name, tt.errMessage, err)
		}

		if len(results) != tt.count {
			t.Errorf("%q. expected %d result objects, got %d", tt.name, tt.count, len(results))
		}
	}
}

func TestReal(t *testing.T) {
	t.Skip("This is a live test, comment this line to run")

	guestbook, err := os.Open("testdata/guestbook.yaml")
	if err != nil {
		t.Fatalf("could not read ./testdata/guestbook.yaml: %v", err)
	}
	defer guestbook.Close()

	c := New(nil)
	c.IncludeThirdPartyAPIs = false
	if err := c.Create("test", guestbook); err != nil {
		t.Fatal(err)
	}

	testSvcEndpointManifest := testServiceManifest + "\n---\n" + testEndpointManifest
	c = New(nil)
	c.IncludeThirdPartyAPIs = false
	if err := c.Create("test-delete", strings.NewReader(testSvcEndpointManifest)); err != nil {
		t.Fatal(err)
	}

	if err := c.Delete("test-delete", strings.NewReader(testEndpointManifest)); err != nil {
		t.Fatal(err)
	}

	// ensures that delete does not fail if a resource is not found
	if err := c.Delete("test-delete", strings.NewReader(testSvcEndpointManifest)); err != nil {
		t.Fatal(err)
	}
}

const testServiceManifest = `
kind: Service
apiVersion: v1
metadata:
  name: my-service
spec:
  selector:
    app: myapp
  ports:
    - port: 80
      protocol: TCP
      targetPort: 9376
`

const testEndpointManifest = `
kind: Endpoints
apiVersion: v1
metadata:
  name: my-service
subsets:
  - addresses:
      - ip: "1.2.3.4"
    ports:
      - port: 9376
`

func createFakePod(name string, labels map[string]string) runtime.Object {
	objectMeta := createObjectMeta(name, labels)

	object := &v1.Pod{
		ObjectMeta: objectMeta,
	}

	return object
}

func createFakeInfo(name string, labels map[string]string) *resource.Info {
	pod := createFakePod(name, labels)
	marshaledObj, _ := json.Marshal(pod)

	mapping := &meta.RESTMapping{
		Resource: name,
		Scope:    meta.RESTScopeNamespace,
		GroupVersionKind: unversioned.GroupVersionKind{
			Kind:    "Pod",
			Version: "v1",
		}}

	client := &fake.RESTClient{
		Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			header := http.Header{}
			header.Set("Content-Type", runtime.ContentTypeJSON)
			return &http.Response{
				StatusCode: 200,
				Header:     header,
				Body:       ioutil.NopCloser(bytes.NewReader(marshaledObj)),
			}, nil
		})}
	info := resource.NewInfo(client, mapping, "default", "nginx", false)

	info.Object = pod

	return info
}

func createObjectMeta(name string, labels map[string]string) v1.ObjectMeta {
	objectMeta := v1.ObjectMeta{Name: name, Namespace: "default"}

	if labels != nil {
		objectMeta.Labels = labels
	}

	return objectMeta
}
