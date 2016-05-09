/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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

package integration

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"k8s.io/kubernetes/test/integration/framework"
)

func TestClient(t *testing.T) {
	_, s := framework.RunAMaster(t)
	defer s.Close()

	resp, err := http.Get(s.URL + "/api/")
	if err != nil {
		t.Fatalf("unexpected error getting api: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %v instead of 200 OK", resp.StatusCode)
	}

	t.Log(kubectl(t, s, "version"))
	t.Log(kubectl(t, s, "cluster-info"))
	t.Log(kubectl(t, s, "get", "pods", "--all-namespaces"))

}

func kubectl(t *testing.T, s *httptest.Server, args ...string) string {
	args = append([]string{"--server", s.URL}, args...)
	out, err := exec.Command("kubectl", args...).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)

}
