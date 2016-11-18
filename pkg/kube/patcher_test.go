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

package kube // import "k8s.io/helm/pkg/kube"

import (
	"testing"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/apimachinery/registered"
	"k8s.io/kubernetes/pkg/apis/extensions/v1beta1"
)

func fakeDep() *v1beta1.Deployment {
	return &v1beta1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name:   "foo",
			Labels: map[string]string{"app": "foo"},
		},
		Spec: v1beta1.DeploymentSpec{
			Selector: &v1beta1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}},
			Template: v1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: map[string]string{"app": "foo"},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "app:v4",
							Image: "abc/app:v4",
							Env:   []v1.EnvVar{{Name: "FOO", Value: "bar"}},
						},
					},
				},
			},
		},
	}
}

func TestCalculatePatch(t *testing.T) {
	aDeployment := fakeDep()
	bDeployment := fakeDep()
	bDeployment.Spec.Template.Spec.Containers[0].Env = []v1.EnvVar{{Name: "BLACK", Value: "magic"}}

	encoder := api.Codecs.LegacyCodec(registered.EnabledVersions()...)
	patch, err := calculatePatch(aDeployment, bDeployment, encoder)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"spec":{"template":{"spec":{"containers":[{"env":[{"name":"BLACK","value":"magic"},{"$patch":"delete","name":"FOO"}],"name":"app:v4"}]}}}}`
	if string(patch) != expected {
		t.Error("unexpected patch output")
	}
}
