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

package kube

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest/fake"
)

var (
	unstructuredSerializer = resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer
	codec                  = scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)
)

func objBody(obj runtime.Object) io.ReadCloser {
	return ioutil.NopCloser(bytes.NewReader([]byte(runtime.EncodeOrDie(codec, obj))))
}

func newPod(name string) v1.Pod {
	return v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: v1.NamespaceDefault,
			SelfLink:  "/api/v1/namespaces/default/pods/" + name,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Name:  "app:v4",
				Image: "abc/app:v4",
				Ports: []v1.ContainerPort{{Name: "http", ContainerPort: 80}},
			}},
		},
	}
}

func newPodList(names ...string) v1.PodList {
	var list v1.PodList
	for _, name := range names {
		list.Items = append(list.Items, newPod(name))
	}
	return list
}

func notFoundBody() *metav1.Status {
	return &metav1.Status{
		Code:    http.StatusNotFound,
		Status:  metav1.StatusFailure,
		Reason:  metav1.StatusReasonNotFound,
		Message: " \"\" not found",
		Details: &metav1.StatusDetails{},
	}
}

func newResponse(code int, obj runtime.Object) (*http.Response, error) {
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)
	body := ioutil.NopCloser(bytes.NewReader([]byte(runtime.EncodeOrDie(codec, obj))))
	return &http.Response{StatusCode: code, Header: header, Body: body}, nil
}

type testClient struct {
	*Client
	*TestFactory
}

func newTestClient() *testClient {
	tf := NewTestFactory()
	c := &Client{Factory: tf, Log: nopLogger}
	return &testClient{Client: c, TestFactory: tf}
}

func TestUpdate(t *testing.T) {
	listA := newPodList("starfish", "otter", "squid")
	listB := newPodList("starfish", "otter", "dolphin")
	listC := newPodList("starfish", "otter", "dolphin")
	listB.Items[0].Spec.Containers[0].Ports = []v1.ContainerPort{{Name: "https", ContainerPort: 443}}
	listC.Items[0].Spec.Containers[0].Ports = []v1.ContainerPort{{Name: "https", ContainerPort: 443}}

	var actions []string

	tf := NewTestFactory().WithNamespace("default")
	defer tf.Cleanup()
	tf.UnstructuredClient = &fake.RESTClient{
		NegotiatedSerializer: unstructuredSerializer,
		Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			p, m := req.URL.Path, req.Method
			actions = append(actions, p+":"+m)
			t.Logf("got request %s %s", p, m)
			switch {
			case p == "/namespaces/default/pods/starfish" && m == "GET":
				return newResponse(200, &listA.Items[0])
			case p == "/namespaces/default/pods/otter" && m == "GET":
				return newResponse(200, &listA.Items[1])
			case p == "/namespaces/default/pods/dolphin" && m == "GET":
				return newResponse(404, notFoundBody())
			case p == "/namespaces/default/pods/starfish" && m == "PATCH":
				data, err := ioutil.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("could not dump request: %s", err)
				}
				req.Body.Close()
				expected := `{"spec":{"$setElementOrder/containers":[{"name":"app:v4"}],"containers":[{"$setElementOrder/ports":[{"containerPort":443}],"name":"app:v4","ports":[{"containerPort":443,"name":"https"},{"$patch":"delete","containerPort":80}]}]}}`
				if string(data) != expected {
					t.Errorf("expected patch\n%s\ngot\n%s", expected, string(data))
				}
				return newResponse(200, &listB.Items[0])
			case p == "/namespaces/default/pods" && m == "POST":
				return newResponse(200, &listB.Items[1])
			case p == "/namespaces/default/pods/squid" && m == "DELETE":
				return newResponse(200, &listB.Items[1])
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				return nil, nil
			}
		}),
	}

	c := &Client{
		Factory: tf,
		Log:     nopLogger,
	}
	if err := c.Update(v1.NamespaceDefault, objBody(&listA), objBody(&listB), false, false, 0, false); err != nil {
		t.Fatal(err)
	}
	// TODO: Find a way to test methods that use Client Set
	// Test with a wait
	// if err := c.Update("test", objBody(codec, &listB), objBody(codec, &listC), false, 300, true); err != nil {
	// 	t.Fatal(err)
	// }
	// Test with a wait should fail
	// TODO: A way to make this not based off of an extremely short timeout?
	// if err := c.Update("test", objBody(codec, &listC), objBody(codec, &listA), false, 2, true); err != nil {
	// 	t.Fatal(err)
	// }
	expectedActions := []string{
		"/namespaces/default/pods/starfish:GET",
		"/namespaces/default/pods/starfish:PATCH",
		"/namespaces/default/pods/otter:GET",
		"/namespaces/default/pods/otter:GET",
		"/namespaces/default/pods/dolphin:GET",
		"/namespaces/default/pods:POST",
		"/namespaces/default/pods/squid:DELETE",
	}
	if len(expectedActions) != len(actions) {
		t.Errorf("unexpected number of requests, expected %d, got %d", len(expectedActions), len(actions))
		return
	}
	for k, v := range expectedActions {
		if actions[k] != v {
			t.Errorf("expected %s request got %s", v, actions[k])
		}
	}
}

func open(t *testing.T, file string) io.Reader {
	f, err := ioutil.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(f)
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		reader    io.Reader
		count     int
		err       bool
	}{
		{
			name:      "Valid input",
			namespace: "test",
			reader:    open(t, "testdata/guestbook.yaml"),
			count:     6,
		}, {
			name:      "Invalid schema",
			namespace: "test",
			reader:    open(t, "testdata/invalid-service.yaml"),
			err:       true,
		},
	}

	c := newTestClient()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.Cleanup()

			// Test for an invalid manifest
			infos, err := c.Build(tt.namespace, tt.reader)
			if err != nil && !tt.err {
				t.Errorf("Got error message when no error should have occurred: %v", err)
			} else if err != nil && strings.Contains(err.Error(), "--validate=false") {
				t.Error("error message was not scrubbed")
			}

			if len(infos) != tt.count {
				t.Errorf("expected %d result objects, got %d", tt.count, len(infos))
			}
		})
	}
}

func TestPerform(t *testing.T) {
	tests := []struct {
		name       string
		reader     io.Reader
		count      int
		err        bool
		errMessage string
	}{
		{
			name:   "Valid input",
			reader: open(t, "testdata/guestbook.yaml"),
			count:  6,
		}, {
			name:       "Empty manifests",
			reader:     strings.NewReader(""),
			err:        true,
			errMessage: "no objects visited",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := []*resource.Info{}

			fn := func(info *resource.Info) error {
				results = append(results, info)
				return nil
			}

			c := newTestClient()
			defer c.Cleanup()
			infos, err := c.Build("default", tt.reader)
			if err != nil && err.Error() != tt.errMessage {
				t.Errorf("Error while building manifests: %v", err)
			}

			err = perform(infos, fn)
			if (err != nil) != tt.err {
				t.Errorf("expected error: %v, got %v", tt.err, err)
			}
			if err != nil && err.Error() != tt.errMessage {
				t.Errorf("expected error message: %v, got %v", tt.errMessage, err)
			}

			if len(results) != tt.count {
				t.Errorf("expected %d result objects, got %d", tt.count, len(results))
			}
		})
	}
}

func TestReal(t *testing.T) {
	t.Skip("This is a live test, comment this line to run")
	c := New(nil)
	if err := c.Create("test", open(t, "testdata/guestbook.yaml"), 300, false); err != nil {
		t.Fatal(err)
	}

	c = New(nil)
	if err := c.Create("test-delete", open(t, "testdata/service.yaml"), 300, false); err != nil {
		t.Fatal(err)
	}

	if err := c.Delete("test-delete", open(t, "testdata/endpoint.yaml")); err != nil {
		t.Fatal(err)
	}

	// ensures that delete does not fail if a resource is not found
	if err := c.Delete("test-delete", open(t, "testdata/service.yaml")); err != nil {
		t.Fatal(err)
	}
}
