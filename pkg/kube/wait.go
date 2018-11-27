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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	corev1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"k8s.io/helm/pkg/kube/internal/deploymentutil"
)

// deployment holds associated replicaSets for a deployment
type deployment struct {
	replicaSets *appsv1.ReplicaSet
	deployment  *appsv1.Deployment
}

// waitForResources polls to get the current status of all pods, PVCs, and Services
// until all are ready or a timeout is reached
func (c *Client) waitForResources(timeout time.Duration, created Result) error {
	c.Log("beginning wait for %d resources with timeout of %v", len(created), timeout)

	kcs, err := c.ClientSet()
	if err != nil {
		return err
	}
	return wait.Poll(2*time.Second, timeout, func() (bool, error) {
		var (
			pods        []corev1.Pod
			services    []corev1.Service
			pvc         []corev1.PersistentVolumeClaim
			deployments []deployment
		)
		for _, v := range created {
			if err := v.Get(); err != nil {
				return false, err
			}
			switch value := asVersioned(v).(type) {
			case *corev1.ReplicationController:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *corev1.Pod:
				pod, err := kcs.CoreV1().Pods(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				pods = append(pods, *pod)
			case *appsv1.Deployment:
				currentDeployment, err := kcs.AppsV1().Deployments(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				// Find RS associated with deployment
				newReplicaSet, err := deploymentutil.GetNewReplicaSet(currentDeployment, kcs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case *appsv1beta1.Deployment:
				currentDeployment, err := kcs.AppsV1().Deployments(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				// Find RS associated with deployment
				newReplicaSet, err := deploymentutil.GetNewReplicaSet(currentDeployment, kcs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case *appsv1beta2.Deployment:
				currentDeployment, err := kcs.AppsV1().Deployments(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				// Find RS associated with deployment
				newReplicaSet, err := deploymentutil.GetNewReplicaSet(currentDeployment, kcs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case *extensions.Deployment:
				currentDeployment, err := kcs.AppsV1().Deployments(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				// Find RS associated with deployment
				newReplicaSet, err := deploymentutil.GetNewReplicaSet(currentDeployment, kcs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case *extensions.DaemonSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1.DaemonSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1beta2.DaemonSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1.StatefulSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1beta1.StatefulSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1beta2.StatefulSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *extensions.ReplicaSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1beta2.ReplicaSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1.ReplicaSet:
				list, err := getPods(kcs, value.Namespace, labels.SelectorFromSet(value.Spec.Selector.MatchLabels).String())
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *corev1.PersistentVolumeClaim:
				claim, err := kcs.CoreV1().PersistentVolumeClaims(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				pvc = append(pvc, *claim)
			case *corev1.Service:
				svc, err := kcs.CoreV1().Services(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				services = append(services, *svc)
			default:
				c.Log("wait: ignoring %s", value.GetObjectKind().GroupVersionKind())
			}
		}
		isReady := c.podsReady(pods) && c.servicesReady(services) && c.volumesReady(pvc) && c.deploymentsReady(deployments)
		return isReady, nil
	})
}

func (c *Client) podsReady(pods []corev1.Pod) bool {
	for _, pod := range pods {
		if !isPodReady(&pod) {
			c.Log("Pod is not ready: %s/%s", pod.GetNamespace(), pod.GetName())
			return false
		}
	}
	return true
}

// isPodReady returns true if a pod is ready; false otherwise.
func isPodReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (c *Client) servicesReady(svc []corev1.Service) bool {
	for _, s := range svc {
		// ExternalName Services are external to cluster so helm shouldn't be checking to see if they're 'ready' (i.e. have an IP Set)
		if s.Spec.Type == corev1.ServiceTypeExternalName {
			continue
		}

		// Make sure the service is not explicitly set to "None" before checking the IP
		if s.Spec.ClusterIP != corev1.ClusterIPNone && !isServiceIPSet(&s) {
			c.Log("Service is not ready: %s/%s", s.GetNamespace(), s.GetName())
			return false
		}
		// This checks if the service has a LoadBalancer and that balancer has an Ingress defined
		if s.Spec.Type == corev1.ServiceTypeLoadBalancer && s.Status.LoadBalancer.Ingress == nil {
			c.Log("Service is not ready: %s/%s", s.GetNamespace(), s.GetName())
			return false
		}
	}
	return true
}

// this function aims to check if the service's ClusterIP is set or not
// the objective is not to perform validation here
func isServiceIPSet(service *corev1.Service) bool {
	return service.Spec.ClusterIP != corev1.ClusterIPNone && service.Spec.ClusterIP != ""
}

func (c *Client) volumesReady(vols []corev1.PersistentVolumeClaim) bool {
	for _, v := range vols {
		if v.Status.Phase != corev1.ClaimBound {
			c.Log("PersistentVolumeClaim is not ready: %s/%s", v.GetNamespace(), v.GetName())
			return false
		}
	}
	return true
}

func (c *Client) deploymentsReady(deployments []deployment) bool {
	for _, v := range deployments {
		if !(v.replicaSets.Status.ReadyReplicas >= *v.deployment.Spec.Replicas-deploymentutil.MaxUnavailable(*v.deployment)) {
			c.Log("Deployment is not ready: %s/%s", v.deployment.GetNamespace(), v.deployment.GetName())
			return false
		}
	}
	return true
}

func getPods(client kubernetes.Interface, namespace, selector string) ([]corev1.Pod, error) {
	list, err := client.CoreV1().Pods(namespace).List(metav1.ListOptions{
		LabelSelector: selector,
	})
	return list.Items, err
}
