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
	"fmt"
	"io"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/kubectl"
	"k8s.io/kubernetes/pkg/kubectl/resource"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util/strategicpatch"
)

// Update reads in the current configuration and a target configuration from io.reader
//  and creates resources that don't already exists, updates resources that have been modified
//  in the target configuration and deletes resources from the current configuration that are
//  not present in the target configuration
//
// Namespace will set the namespaces
func Update(c *Client, namespace string, currentReader, targetReader io.Reader) error {
	currentInfos, err := c.newBuilder(namespace, currentReader).Do().Infos()
	if err != nil {
		return fmt.Errorf("failed decoding reader into objects: %s", err)
	}

	target := c.newBuilder(namespace, targetReader).Do()
	if target.Err() != nil {
		return fmt.Errorf("failed decoding reader into objects: %s", target.Err())
	}

	encoder := c.JSONEncoder()

	var targetInfos []*resource.Info

	err = target.Visit(func(info *resource.Info, err error) error {
		targetInfos = append(targetInfos, info)
		if err != nil {
			return err
		}

		// Get the modified configuration of the object. Embed the result
		// as an annotation in the modified configuration, so that it will appear
		// in the patch sent to the server.
		modified, err := kubectl.GetModifiedConfiguration(info, true, encoder)
		if err != nil {
			return err
		}
		if err := info.Get(); err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("retrieving current configuration of:\n%v\nfrom server: %v", info, err)
			}
			return createResource(info, encoder)
		}

		// Serialize the current configuration of the object from the server.
		current, err := runtime.Encode(encoder, info.Object)
		if err != nil {
			return fmt.Errorf("serializing current configuration from:\n%v\n: %v", info.Object, err)
		}

		// Retrieve the original configuration of the object from the annotation.
		original, err := kubectl.GetOriginalConfiguration(info.Mapping, info.Object)
		if err != nil {
			return fmt.Errorf("retrieving original configuration from:\n%v\n: %v", info.Object, err)
		}

		versionedObject, err := info.Mapping.ConvertToVersion(info.Object, info.Mapping.GroupVersionKind.GroupVersion())
		if err != nil {
			return fmt.Errorf("converting encoded server-side object back to versioned struct:\n%v\n: %v", info.Object, err)
		}

		// Compute a three way strategic merge patch to send to server.
		patch, err := strategicpatch.CreateThreeWayMergePatch(original, modified, current, versionedObject, true)
		if err != nil {
			format := "creating patch with:\noriginal:\n%s\nmodified:\n%s\ncurrent:\n%s\n: %v"
			return fmt.Errorf(format, original, modified, current, err)
		}

		// send patch to server
		helper := resource.NewHelper(info.Client, info.Mapping)
		_, err = helper.Patch(info.Namespace, info.Name, api.StrategicMergePatchType, patch)
		return err
	})

	if err != nil {
		return err
	}
	for _, info := range deletedResources(currentInfos, targetInfos) {
		if err := deleteResource(c, info); err != nil {
			return err
		}
	}
	return nil
}
