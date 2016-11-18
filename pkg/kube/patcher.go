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
	"bytes"
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util/strategicpatch"
)

func calculatePatch(a, b runtime.Object, encoder runtime.Encoder) ([]byte, error) {
	_, unversioned, err := api.Scheme.ObjectKind(a)
	switch {
	case err != nil:
		return nil, err
	case unversioned:
		return nil, fmt.Errorf("must use a versioned object")
	}
	aBytes, err := runtime.Encode(encoder, a)
	if err != nil {
		return nil, err
	}

	bBytes, err := runtime.Encode(encoder, b)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(aBytes, bBytes) {
		return nil, nil
	}
	return strategicpatch.CreateStrategicMergePatch(aBytes, bBytes, a)
}
