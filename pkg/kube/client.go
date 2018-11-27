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
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"time"

	"github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	watchtools "k8s.io/client-go/tools/watch"

	"k8s.io/helm/pkg/kube/factory"
)

// ErrNoObjectsVisited indicates that during a visit operation, no matching objects were found.
var ErrNoObjectsVisited = errors.New("no objects visited")

// Client represents a client capable of communicating with the Kubernetes API.
type Client struct {
	Factory factory.Factory
	Log     func(string, ...interface{})
}

// New creates a new Client.
func New(getter genericclioptions.RESTClientGetter) *Client {
	if getter == nil {
		getter = genericclioptions.NewConfigFlags()
	}
	return &Client{
		Factory: factory.New(getter),
		Log:     nopLogger,
	}
}

func (c *Client) ClientSet() (*kubernetes.Clientset, error) {
	return c.Factory.ClientSet()
}

var nopLogger = func(_ string, _ ...interface{}) {}

// resourceActorFunc performs an action on a single resource.
type resourceActorFunc func(*resource.Info) error

// Create creates Kubernetes resources from an io.reader.
//
// Namespace will set the namespace.
func (c *Client) Create(namespace string, reader io.Reader, timeout int64, shouldWait bool) error {
	c.Log("building resources from manifest")
	infos, err := c.BuildUnstructured(namespace, reader)
	if err != nil {
		return err
	}
	c.Log("creating %d resource(s)", len(infos))
	if err := perform(infos, createResource); err != nil {
		return err
	}
	if shouldWait {
		return c.waitForResources(time.Duration(timeout)*time.Second, infos)
	}
	return nil
}

func (c *Client) namespace() string {
	if ns, _, err := c.Factory.ToRawKubeConfigLoader().Namespace(); err == nil {
		return ns
	}
	return corev1.NamespaceDefault
}

// newBuilder returns a new resource builder for structured api objects.
func (c *Client) newBuilder() *resource.Builder {
	return c.Factory.NewBuilder().
		ContinueOnError().
		NamespaceParam(c.namespace()).
		DefaultNamespace().
		RequireNamespace().
		Flatten()
}

// BuildUnstructured validates for Kubernetes objects and returns unstructured infos.
func (c *Client) BuildUnstructured(namespace string, reader io.Reader) (Result, error) {
	var result Result

	result, err := c.newBuilder().
		Unstructured().
		Stream(reader, "").
		Do().Infos()
	return result, err
}

// Build validates for Kubernetes objects and returns resource Infos from a io.Reader.
func (c *Client) Build(namespace string, reader io.Reader) (Result, error) {

	b := c.newBuilder()
	schema, err := c.Factory.Validator(true)
	b.AddError(err)

	var result Result
	result, err = b.
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		Schema(schema).
		Stream(reader, "").
		Do().
		Infos()
	return result, scrubValidationError(err)
}

// Get gets Kubernetes resources as pretty-printed string.
//
// Namespace will set the namespace.
func (c *Client) Get(namespace string, reader io.Reader) (string, error) {
	return "", errors.New("not implemented")
}

// Update reads in the current configuration and a target configuration from io.reader
// and creates resources that don't already exists, updates resources that have been modified
// in the target configuration and deletes resources from the current configuration that are
// not present in the target configuration.
//
// Namespace will set the namespaces.
func (c *Client) Update(namespace string, originalReader, targetReader io.Reader, force, recreate bool, timeout int64, shouldWait bool) error {
	original, err := c.BuildUnstructured(namespace, originalReader)
	if err != nil {
		return errors.Wrap(err, "failed decoding reader into objects")
	}

	c.Log("building resources from updated manifest")
	target, err := c.BuildUnstructured(namespace, targetReader)
	if err != nil {
		return errors.Wrap(err, "failed decoding reader into objects")
	}

	updateErrors := []string{}

	c.Log("checking %d resources for changes", len(target))
	err = target.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}

		helper := resource.NewHelper(info.Client, info.Mapping)
		if _, err := helper.Get(info.Namespace, info.Name, info.Export); err != nil {
			if !apierrors.IsNotFound(err) {
				return errors.Wrap(err, "could not get information about the resource")
			}

			// Since the resource does not exist, create it.
			if err := createResource(info); err != nil {
				return errors.Wrap(err, "failed to create resource")
			}

			kind := info.Mapping.GroupVersionKind.Kind
			c.Log("Created a new %s called %q\n", kind, info.Name)
			return nil
		}

		originalInfo := original.Get(info)
		if originalInfo == nil {
			kind := info.Mapping.GroupVersionKind.Kind
			return errors.Errorf("no %s with the name %q found", kind, info.Name)
		}

		if err := updateResource(c, info, originalInfo.Object, force, recreate); err != nil {
			c.Log("error updating the resource %q:\n\t %v", info.Name, err)
			updateErrors = append(updateErrors, err.Error())
		}

		return nil
	})

	switch {
	case err != nil:
		return err
	case len(updateErrors) != 0:
		return errors.Errorf(strings.Join(updateErrors, " && "))
	}

	for _, info := range original.Difference(target) {
		c.Log("Deleting %q in %s...", info.Name, info.Namespace)
		if err := deleteResource(info); err != nil {
			c.Log("Failed to delete %q, err: %s", info.Name, err)
		}
	}
	if shouldWait {
		return c.waitForResources(time.Duration(timeout)*time.Second, target)
	}
	return nil
}

// Delete deletes Kubernetes resources from an io.reader.
//
// Namespace will set the namespace.
func (c *Client) Delete(namespace string, reader io.Reader) error {
	infos, err := c.BuildUnstructured(namespace, reader)
	if err != nil {
		return err
	}
	return perform(infos, func(info *resource.Info) error {
		c.Log("Starting delete for %q %s", info.Name, info.Mapping.GroupVersionKind.Kind)
		err := deleteResource(info)
		return c.skipIfNotFound(err)
	})
}

func (c *Client) skipIfNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		c.Log("%v", err)
		return nil
	}
	return err
}

func (c *Client) watchTimeout(t time.Duration) resourceActorFunc {
	return func(info *resource.Info) error {
		return c.watchUntilReady(t, info)
	}
}

// WatchUntilReady watches the resource given in the reader, and waits until it is ready.
//
// This function is mainly for hook implementations. It watches for a resource to
// hit a particular milestone. The milestone depends on the Kind.
//
// For most kinds, it checks to see if the resource is marked as Added or Modified
// by the Kubernetes event stream. For some kinds, it does more:
//
// - Jobs: A job is marked "Ready" when it has successfully completed. This is
//   ascertained by watching the Status fields in a job's output.
//
// Handling for other kinds will be added as necessary.
func (c *Client) WatchUntilReady(namespace string, reader io.Reader, timeout int64, shouldWait bool) error {
	infos, err := c.Build(namespace, reader)
	if err != nil {
		return err
	}
	// For jobs, there's also the option to do poll c.Jobs(namespace).Get():
	// https://github.com/adamreese/kubernetes/blob/master/test/e2e/job.go#L291-L300
	return perform(infos, c.watchTimeout(time.Duration(timeout)*time.Second))
}

func perform(infos Result, fn resourceActorFunc) error {
	if len(infos) == 0 {
		return ErrNoObjectsVisited
	}

	for _, info := range infos {
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}

func createResource(info *resource.Info) error {
	obj, err := resource.NewHelper(info.Client, info.Mapping).Create(info.Namespace, true, info.Object, nil)
	if err != nil {
		return err
	}
	return info.Refresh(obj, true)
}

func deleteResource(info *resource.Info) error {
	policy := metav1.DeletePropagationBackground
	opts := &metav1.DeleteOptions{PropagationPolicy: &policy}
	_, err := resource.NewHelper(info.Client, info.Mapping).DeleteWithOptions(info.Namespace, info.Name, opts)
	return err
}

func createPatch(target *resource.Info, current runtime.Object) ([]byte, types.PatchType, error) {
	oldData, err := json.Marshal(current)
	if err != nil {
		return nil, types.StrategicMergePatchType, errors.Wrap(err, "serializing current configuration")
	}
	newData, err := json.Marshal(target.Object)
	if err != nil {
		return nil, types.StrategicMergePatchType, errors.Wrap(err, "serializing target configuration")
	}

	// While different objects need different merge types, the parent function
	// that calls this does not try to create a patch when the data (first
	// returned object) is nil. We can skip calculating the merge type as
	// the returned merge type is ignored.
	if apiequality.Semantic.DeepEqual(oldData, newData) {
		return nil, types.StrategicMergePatchType, nil
	}

	// Get a versioned object
	versionedObject := asVersioned(target)

	// Unstructured objects, such as CRDs, may not have an not registered error
	// returned from ConvertToVersion. Anything that's unstructured should
	// use the jsonpatch.CreateMergePatch. Strategic Merge Patch is not supported
	// on objects like CRDs.
	_, isUnstructured := versionedObject.(runtime.Unstructured)

	switch {
	case runtime.IsNotRegisteredError(err), isUnstructured:
		// fall back to generic JSON merge patch
		patch, err := jsonpatch.CreateMergePatch(oldData, newData)
		return patch, types.MergePatchType, err
	case err != nil:
		return nil, types.StrategicMergePatchType, errors.Wrap(err, "failed to get versionedObject")
	default:
		patch, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, versionedObject)
		return patch, types.StrategicMergePatchType, err
	}
}

func updateResource(c *Client, target *resource.Info, currentObj runtime.Object, force, recreate bool) error {
	patch, patchType, err := createPatch(target, currentObj)
	if err != nil {
		return errors.Wrap(err, "failed to create patch")
	}
	if patch == nil {
		c.Log("Looks like there are no changes for %s %q", target.Mapping.GroupVersionKind.Kind, target.Name)
		// This needs to happen to make sure that tiller has the latest info from the API
		// Otherwise there will be no labels and other functions that use labels will panic
		if err := target.Get(); err != nil {
			return errors.Wrap(err, "error trying to refresh resource information")
		}
	} else {
		// send patch to server
		helper := resource.NewHelper(target.Client, target.Mapping)

		obj, err := helper.Patch(target.Namespace, target.Name, patchType, patch, nil)
		if err != nil {
			kind := target.Mapping.GroupVersionKind.Kind
			log.Printf("Cannot patch %s: %q (%v)", kind, target.Name, err)

			if force {
				// Attempt to delete...
				if err := deleteResource(target); err != nil {
					return err
				}
				log.Printf("Deleted %s: %q", kind, target.Name)

				// ... and recreate
				if err := createResource(target); err != nil {
					return errors.Wrap(err, "failed to recreate resource")
				}
				log.Printf("Created a new %s called %q\n", kind, target.Name)

				// No need to refresh the target, as we recreated the resource based
				// on it. In addition, it might not exist yet and a call to `Refresh`
				// may fail.
			} else {
				log.Print("Use --force to force recreation of the resource")
				return err
			}
		} else {
			// When patch succeeds without needing to recreate, refresh target.
			target.Refresh(obj, true)
		}
	}

	if !recreate {
		return nil
	}

	versioned := asVersioned(target)
	selector, err := getSelectorFromObject(versioned)
	if err != nil {
		return nil
	}

	client, err := c.ClientSet()
	if err != nil {
		return err
	}

	pods, err := client.CoreV1().Pods(target.Namespace).List(metav1.ListOptions{
		LabelSelector: labels.Set(selector).AsSelector().String(),
	})
	if err != nil {
		return err
	}

	// Restart pods
	for _, pod := range pods.Items {
		c.Log("Restarting pod: %v/%v", pod.Namespace, pod.Name)

		// Delete each pod for get them restarted with changed spec.
		if err := client.CoreV1().Pods(pod.Namespace).Delete(pod.Name, metav1.NewPreconditionDeleteOptions(string(pod.UID))); err != nil {
			return err
		}
	}
	return nil
}

func getSelectorFromObject(obj runtime.Object) (map[string]string, error) {
	switch typed := obj.(type) {

	case *corev1.ReplicationController:
		return typed.Spec.Selector, nil

	case *extv1beta1.ReplicaSet:
		return typed.Spec.Selector.MatchLabels, nil
	case *appsv1.ReplicaSet:
		return typed.Spec.Selector.MatchLabels, nil

	case *extv1beta1.Deployment:
		return typed.Spec.Selector.MatchLabels, nil
	case *appsv1beta1.Deployment:
		return typed.Spec.Selector.MatchLabels, nil
	case *appsv1beta2.Deployment:
		return typed.Spec.Selector.MatchLabels, nil
	case *appsv1.Deployment:
		return typed.Spec.Selector.MatchLabels, nil

	case *extv1beta1.DaemonSet:
		return typed.Spec.Selector.MatchLabels, nil
	case *appsv1beta2.DaemonSet:
		return typed.Spec.Selector.MatchLabels, nil
	case *appsv1.DaemonSet:
		return typed.Spec.Selector.MatchLabels, nil

	case *batchv1.Job:
		return typed.Spec.Selector.MatchLabels, nil

	case *appsv1beta1.StatefulSet:
		return typed.Spec.Selector.MatchLabels, nil
	case *appsv1beta2.StatefulSet:
		return typed.Spec.Selector.MatchLabels, nil
	case *appsv1.StatefulSet:
		return typed.Spec.Selector.MatchLabels, nil

	default:
		return nil, errors.Errorf("unsupported kind when getting selector: %v", obj)
	}
}

func (c *Client) watchUntilReady(timeout time.Duration, info *resource.Info) error {
	w, err := resource.NewHelper(info.Client, info.Mapping).WatchSingle(info.Namespace, info.Name, info.ResourceVersion)
	if err != nil {
		return err
	}

	kind := info.Mapping.GroupVersionKind.Kind
	c.Log("Watching for changes to %s %s with timeout of %v", kind, info.Name, timeout)

	// What we watch for depends on the Kind.
	// - For a Job, we watch for completion.
	// - For all else, we watch until Ready.
	// In the future, we might want to add some special logic for types
	// like Ingress, Volume, etc.

	ctx, cancel := watchtools.ContextWithOptionalTimeout(context.Background(), timeout)
	defer cancel()
	_, err = watchtools.UntilWithoutRetry(ctx, w, func(e watch.Event) (bool, error) {
		switch e.Type {
		case watch.Added, watch.Modified:
			// For things like a secret or a config map, this is the best indicator
			// we get. We care mostly about jobs, where what we want to see is
			// the status go into a good state. For other types, like ReplicaSet
			// we don't really do anything to support these as hooks.
			c.Log("Add/Modify event for %s: %v", info.Name, e.Type)
			if kind == "Job" {
				return c.waitForJob(e, info.Name)
			}
			return true, nil
		case watch.Deleted:
			c.Log("Deleted event for %s", info.Name)
			return true, nil
		case watch.Error:
			// Handle error and return with an error.
			c.Log("Error event for %s", info.Name)
			return true, errors.Errorf("failed to deploy %s", info.Name)
		default:
			return false, nil
		}
	})
	return err
}

// waitForJob is a helper that waits for a job to complete.
//
// This operates on an event returned from a watcher.
func (c *Client) waitForJob(e watch.Event, name string) (bool, error) {
	o, ok := e.Object.(*batchv1.Job)
	if !ok {
		return true, errors.Errorf("expected %s to be a *batch.Job, got %T", name, e.Object)
	}

	for _, c := range o.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true, nil
		} else if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true, errors.Errorf("job failed: %s", c.Reason)
		}
	}

	c.Log("%s: Jobs active: %d, jobs failed: %d, jobs succeeded: %d", name, o.Status.Active, o.Status.Failed, o.Status.Succeeded)
	return false, nil
}

// scrubValidationError removes kubectl info from the message.
func scrubValidationError(err error) error {
	if err == nil {
		return nil
	}
	const stopValidateMessage = "if you choose to ignore these errors, turn validation off with --validate=false"

	if strings.Contains(err.Error(), stopValidateMessage) {
		return errors.New(strings.Replace(err.Error(), "; "+stopValidateMessage, "", -1))
	}
	return err
}

// WaitAndGetCompletedPodPhase waits up to a timeout until a pod enters a completed phase
// and returns said phase (PodSucceeded or PodFailed qualify).
func (c *Client) WaitAndGetCompletedPodPhase(namespace string, reader io.Reader, timeout time.Duration) (corev1.PodPhase, error) {
	infos, err := c.Build(namespace, reader)
	if err != nil {
		return corev1.PodUnknown, err
	}
	info := infos[0]

	kind := info.Mapping.GroupVersionKind.Kind
	if kind != "Pod" {
		return corev1.PodUnknown, errors.Errorf("%s is not a Pod", info.Name)
	}

	if err := c.watchPodUntilComplete(timeout, info); err != nil {
		return corev1.PodUnknown, err
	}

	if err := info.Get(); err != nil {
		return corev1.PodUnknown, err
	}
	status := info.Object.(*corev1.Pod).Status.Phase

	return status, nil
}

func (c *Client) watchPodUntilComplete(timeout time.Duration, info *resource.Info) error {
	w, err := resource.NewHelper(info.Client, info.Mapping).WatchSingle(info.Namespace, info.Name, info.ResourceVersion)
	if err != nil {
		return err
	}

	c.Log("Watching pod %s for completion with timeout of %v", info.Name, timeout)
	ctx, cancel := watchtools.ContextWithOptionalTimeout(context.Background(), timeout)
	defer cancel()
	_, err = watchtools.UntilWithoutRetry(ctx, w, func(e watch.Event) (bool, error) {
		switch e.Type {
		case watch.Deleted:
			return false, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "")
		}
		switch t := e.Object.(type) {
		case *corev1.Pod:
			switch t.Status.Phase {
			case corev1.PodFailed, corev1.PodSucceeded:
				return true, nil
			}
		}
		return false, nil
	})
	return err
}
