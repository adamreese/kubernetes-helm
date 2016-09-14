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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"

	"github.com/Masterminds/semver"
	"github.com/docker/distribution/reference"
)

func ResolveTag(ver string, refs []string) (string, error) {
	// Create the constraint first to make sure it's valid before
	// working on the repo.
	constraint, err := semver.NewConstraint(ver)
	if err != nil {
		return "", err
	}

	// Convert and filter the list to semver.Version instances
	semvers := getSemVers(refs)

	// Sort semver list
	sort.Sort(sort.Reverse(semver.Collection(semvers)))

	found := false
	for _, v := range semvers {
		if constraint.Check(v) {
			found = true
			// If the constrint passes get the original reference
			ver = v.Original()
			break
		}
	}
	if !found {
		return "", errors.New("could not find a matching version")
	}
	return ver, nil
}

type RegistryClient struct {
	httpClient *http.Client
}

func NewRegistryClient() *RegistryClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	return &RegistryClient{
		httpClient: client,
	}
}

func (c *RegistryClient) GetTags(ref string) ([]string, error) {
	host, name, err := ParseImageName(ref)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Get("https://" + host + "/v2/" + name + "/tags/list")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var j struct{ Tags []string }
	json.Unmarshal(body, &j)
	return j.Tags, nil
}

// ParseImageName parses a docker image string into two parts: host and name
func ParseImageName(image string) (string, string, error) {
	named, err := reference.ParseNamed(image)
	if err != nil {
		return "", "", fmt.Errorf("couldn't parse image name: %v", err)
	}

	hostname, name := reference.SplitHostname(named)
	return hostname, name, nil
}

func isSemver(tag string) bool {
	_, err := semver.NewVersion(tag)
	return err == nil
}

// Filter a list of versions to only included semantic versions. The response
// is a mapping of the original version to the semantic version.
func getSemVers(refs []string) []*semver.Version {
	sv := []*semver.Version{}
	for _, r := range refs {
		v, err := semver.NewVersion(r)
		if err == nil {
			sv = append(sv, v)
		}
	}
	return sv
}
