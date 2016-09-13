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

package installer // import "k8s.io/helm/cmd/helm/installer"

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetTags(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"tags": ["canary", "1.2.3", "1.0", "1.3"]}`)
	}))
	defer ts.Close()

	ref := strings.TrimPrefix(ts.URL+"/foo", "https://")

	tags, err := NewRegistryClient().GetTags(ref)
	if err != nil {
		t.Fatal(err)
	}

	if len(tags) != 4 {
		t.Errorf("expected 4 tags, got %d", len(tags))
	}
}

func TestResolveTag(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"tags": ["canary", "v1.2.3", "1.0", "1.3"]}`)
	}))
	defer ts.Close()

	ref := strings.TrimPrefix(ts.URL+"/foo", "https://")

	tags, err := NewRegistryClient().GetTags(ref)
	if err != nil {
		t.Fatal(err)
	}

	tag, err := ResolveTag("~1.2", tags)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "1.2.3" {
		t.Fatalf("expected '1.2.3', got '%s'", tag)
	}
}
