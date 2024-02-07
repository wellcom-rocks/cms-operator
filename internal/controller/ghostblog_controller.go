/*
Copyright 2024.

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

package controller

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cmsv1alpha1 "github.com/wellcom-rocks/cms-operator/api/v1alpha1"
)

const ghostblogFinalizer = "cms.wellcom.rocks/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableGhostBlog represents the status of the Deployment reconciliation
	typeAvailableGhostBlog = "Available"
	// typeDegradedGhostBlog represents the status used when the custom resource is deleted and the finalizer operations are must to occur.
	typeDegradedGhostBlog = "Degraded"
)

// GhostBlogReconciler reconciles a GhostBlog object
type GhostBlogReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=cms.wellcom.rocks,resources=ghostblogs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cms.wellcom.rocks,resources=ghostblogs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cms.wellcom.rocks,resources=ghostblogs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

func (r *GhostBlogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the GhostBlog instance
	// The purpose is check if the Custom Resource for the Kind GhostBlog
	// is applied on the cluster if not we return nil to stop the reconciliation
	ghostblog := &cmsv1alpha1.GhostBlog{}
	err := r.Get(ctx, req.NamespacedName, ghostblog)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("ghostblog resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get ghostblog")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status are available
	if ghostblog.Status.Conditions == nil || len(ghostblog.Status.Conditions) == 0 {
		meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeAvailableGhostBlog, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, ghostblog); err != nil {
			log.Error(err, "Failed to update GhostBlog status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the ghostblog Custom Resource after update the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raise the issue "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, ghostblog); err != nil {
			log.Error(err, "Failed to re-fetch ghostblog")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occurs before the custom resource to be deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(ghostblog, ghostblogFinalizer) {
		log.Info("Adding Finalizer for GhostBlog")
		if ok := controllerutil.AddFinalizer(ghostblog, ghostblogFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, ghostblog); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the GhostBlog instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isGhostBlogMarkedToBeDeleted := ghostblog.GetDeletionTimestamp() != nil
	if isGhostBlogMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(ghostblog, ghostblogFinalizer) {
			log.Info("Performing Finalizer Operations for GhostBlog before delete CR")

			// Let's add here an status "Downgrade" to define that this resource begin its process to be terminated.
			meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeDegradedGhostBlog,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", ghostblog.Name)})

			if err := r.Status().Update(ctx, ghostblog); err != nil {
				log.Error(err, "Failed to update GhostBlog status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			r.doFinalizerOperationsForGhostBlog(ghostblog)

			// TODO(user): If you add operations to the doFinalizerOperationsForGhostBlog method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the ghostblog Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, ghostblog); err != nil {
				log.Error(err, "Failed to re-fetch ghostblog")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeDegradedGhostBlog,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", ghostblog.Name)})

			if err := r.Status().Update(ctx, ghostblog); err != nil {
				log.Error(err, "Failed to update GhostBlog status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for GhostBlog after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(ghostblog, ghostblogFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for GhostBlog")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, ghostblog); err != nil {
				log.Error(err, "Failed to remove finalizer for GhostBlog")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: ghostblog.Name, Namespace: ghostblog.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.deploymentForGhostBlog(ghostblog)
		if err != nil {
			log.Error(err, "Failed to define new Deployment resource for GhostBlog")

			// The following implementation will update the status
			meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeAvailableGhostBlog,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", ghostblog.Name, err)})

			if err := r.Status().Update(ctx, ghostblog); err != nil {
				log.Error(err, "Failed to update GhostBlog status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Deployment",
			"Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment",
				"Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}

		// Deployment created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// Check if the PersistentVolumeClaim already exists, if not create a new one
	foundPVC := &corev1.PersistentVolumeClaim{}
	err = r.Get(ctx, types.NamespacedName{Name: ghostblog.Name + "-pvc", Namespace: ghostblog.Namespace}, foundPVC)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new PersistantVolumelaim
		pvc, err := r.persistantVolumeClaimForGhostBlog(ghostblog)
		if err != nil {
			log.Error(err, "Failed to define new PVC resource for GhostBlog")

			// The following implementation will update the status
			meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeAvailableGhostBlog,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create PVC for the custom resource (%s): (%s)", ghostblog.Name, err)})

			if err := r.Status().Update(ctx, ghostblog); err != nil {
				log.Error(err, "Failed to update GhostBlog status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new PVC",
			"PVC.Namespace", pvc.Namespace, "PVC.Name", pvc.Name)
		if err = r.Create(ctx, pvc); err != nil {
			log.Error(err, "Failed to create new PVC",
				"PVC.Namespace", pvc.Namespace, "PVC.Name", pvc.Name)
			return ctrl.Result{}, err
		}

		// PVC created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get PVC")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// Check if Servie already exists, if not create a new one
	foundSVC := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: ghostblog.Name, Namespace: ghostblog.Namespace}, foundSVC)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new Service
		svc, err := r.serviceForGhostBlog(ghostblog)
		if err != nil {
			log.Error(err, "Failed to define new SVC resource for GhostBlog")

			// The following implementation will update the status
			meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeAvailableGhostBlog,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create SVC for the custom resource (%s): (%s)", ghostblog.Name, err)})

			if err := r.Status().Update(ctx, ghostblog); err != nil {
				log.Error(err, "Failed to update GhostBlog status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new PVC",
			"PVC.Namespace", svc.Namespace, "PVC.Name", svc.Name)
		if err = r.Create(ctx, svc); err != nil {
			log.Error(err, "Failed to create new PVC",
				"PVC.Namespace", svc.Namespace, "PVC.Name", svc.Name)
			return ctrl.Result{}, err
		}

		// PVC created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get SVC")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// Check if Ingress already exists, if not create a new one
	foundIngress := &networkv1.Ingress{}
	err = r.Get(ctx, types.NamespacedName{Name: ghostblog.Name, Namespace: ghostblog.Namespace}, foundIngress)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new Ingress
		ing, err := r.ingressForGhostBlog(ghostblog)
		if err != nil {
			log.Error(err, "Failed to define new Ingress resource for GhostBlog")

			// The following implementation will update the status
			meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeAvailableGhostBlog,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Ingress for the custom resource (%s): (%s)", ghostblog.Name, err)})
			if err := r.Status().Update(ctx, ghostblog); err != nil {
				log.Error(err, "Failed to update GhostBlog status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Ingress", "Ingress.Namespace", ing.Namespace, "Ingress.Name", ing.Name)
		if err = r.Create(ctx, ing); err != nil {
			log.Error(err, "Failed to create new Ingress", "Ingress.Namespace", ing.Namespace, "Ingress.Name", ing.Name)
			return ctrl.Result{}, err
		}

		// Ingress created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Ingress")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// The CRD API is defining that the GhostBlog type, have a GhostBlogSpec.Size field
	// to set the quantity of Deployment instances is the desired state on the cluster.
	// Therefore, the following code will ensure the Deployment size is the same as defined
	// via the Size spec of the Custom Resource which we are reconciling.
	size := ghostblog.Spec.Size
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		if err = r.Update(ctx, found); err != nil {
			log.Error(err, "Failed to update Deployment",
				"Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)

			// Re-fetch the ghostblog Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, ghostblog); err != nil {
				log.Error(err, "Failed to re-fetch ghostblog")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeAvailableGhostBlog,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update the size for the custom resource (%s): (%s)", ghostblog.Name, err)})

			if err := r.Status().Update(ctx, ghostblog); err != nil {
				log.Error(err, "Failed to update GhostBlog status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Now, that we update the size we want to requeue the reconciliation
		// so that we can ensure that we have the latest state of the resource before
		// update. Also, it will help ensure the desired state on the cluster
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&ghostblog.Status.Conditions, metav1.Condition{Type: typeAvailableGhostBlog,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", ghostblog.Name, size)})

	if err := r.Status().Update(ctx, ghostblog); err != nil {
		log.Error(err, "Failed to update GhostBlog status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeGhostBlog will perform the required operations before delete the CR.
func (r *GhostBlogReconciler) doFinalizerOperationsForGhostBlog(cr *cmsv1alpha1.GhostBlog) {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of delete resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as depended of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))
}

// deploymentForGhostBlog returns a GhostBlog Deployment object
func (r *GhostBlogReconciler) deploymentForGhostBlog(
	ghostblog *cmsv1alpha1.GhostBlog) (*appsv1.Deployment, error) {
	ls := labelsForGhostBlog(ghostblog.Name, ghostblog.Spec.Image)
	replicas := ghostblog.Spec.Size

	// Get the Operand image
	image, err := imageForGhostBlog(ghostblog.Spec.Image)
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ghostblog.Name,
			Namespace: ghostblog.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						// IMPORTANT: seccomProfile was introduced with Kubernetes 1.19
						// If you are looking for to produce solutions to be supported
						// on lower versions you must remove this option.
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Volumes: volumeForGhostBlog(ghostblog),
					Containers: []corev1.Container{{
						Image:           image,
						Name:            "ghostblog",
						ImagePullPolicy: corev1.PullIfNotPresent,
						// Ensure restrictive context for the container
						// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
						SecurityContext: &corev1.SecurityContext{
							// WARNING: Ensure that the image used defines an UserID in the Dockerfile
							// otherwise the Pod will not run and will fail with "container has runAsNonRoot and image has non-numeric user"".
							// If you want your workloads admitted in namespaces enforced with the restricted mode in OpenShift/OKD vendors
							// then, you MUST ensure that the Dockerfile defines a User ID OR you MUST leave the "RunAsNonRoot" and
							// "RunAsUser" fields empty.
							RunAsNonRoot: &[]bool{true}[0],
							// The ghostblog image does not use a non-zero numeric user as the default user.
							// Due to RunAsNonRoot field being set to true, we need to force the user in the
							// container to a non-zero numeric user. We do this using the RunAsUser field.
							// However, if you are looking to provide solution for K8s vendors like OpenShift
							// be aware that you cannot run under its restricted-v2 SCC if you set this value.
							RunAsUser:                &[]int64{1001}[0],
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Ports: []corev1.ContainerPort{{
							ContainerPort: ghostblog.Spec.ContainerPort,
							Name:          "ghostblog",
						}},
						Env:          envVariablesForGhostBlog(ghostblog),
						VolumeMounts: volumeMountsForGhostBlog(ghostblog),
					}},
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(ghostblog, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

func (r *GhostBlogReconciler) persistantVolumeClaimForGhostBlog(
	ghostblog *cmsv1alpha1.GhostBlog) (*corev1.PersistentVolumeClaim, error) {
	// ls := labelsForGhostBlog(ghostblog.Name, ghostblog.Spec.Image)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ghostblog.Name + "-pvc",
			Namespace: ghostblog.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: ghostblog.Spec.Persistent.Size,
				},
			},
		},
	}

	// Set the ownerRef for the PVC
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(ghostblog, pvc, r.Scheme); err != nil {
		return nil, err
	}
	return pvc, nil
}

func (r *GhostBlogReconciler) serviceForGhostBlog(
	ghostblog *cmsv1alpha1.GhostBlog) (*corev1.Service, error) {
	ls := labelsForGhostBlog(ghostblog.Name, ghostblog.Spec.Image)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ghostblog.Name,
			Namespace: ghostblog.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{
				Port:       ghostblog.Spec.ContainerPort,
				TargetPort: intstr.FromInt(int(ghostblog.Spec.ContainerPort)),
			}},
		},
	}

	// Set the ownerRef for the Service
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(ghostblog, svc, r.Scheme); err != nil {
		return nil, err
	}
	return svc, nil
}

func (r *GhostBlogReconciler) ingressForGhostBlog(
	ghostblog *cmsv1alpha1.GhostBlog) (*networkv1.Ingress, error) {

	pathType := networkv1.PathTypePrefix

	annotations := map[string]string{}
	if ghostblog.Spec.Ingress.Annotations != nil {
		annotations = ghostblog.Spec.Ingress.Annotations
	}

	tls := []networkv1.IngressTLS{}
	if ghostblog.Spec.Ingress.TLS.Enabled {
		tls = append(tls, networkv1.IngressTLS{
			Hosts:      ghostblog.Spec.Ingress.Hosts,
			SecretName: ghostblog.Spec.Ingress.TLS.SecretName,
		})
	}

	hosts := []string{}
	if ghostblog.Spec.Ingress.Hosts != nil {
		hosts = ghostblog.Spec.Ingress.Hosts
	}

	rules := []networkv1.IngressRule{}
	for _, host := range hosts {
		rules = append(rules, networkv1.IngressRule{
			Host: host,
			IngressRuleValue: networkv1.IngressRuleValue{
				HTTP: &networkv1.HTTPIngressRuleValue{
					Paths: []networkv1.HTTPIngressPath{
						{
							PathType: &pathType,
							Path:     "/",
							Backend: networkv1.IngressBackend{
								Service: &networkv1.IngressServiceBackend{
									Name: ghostblog.Name,
									Port: networkv1.ServiceBackendPort{
										Number: ghostblog.Spec.ContainerPort,
									},
								},
							},
						},
					},
				},
			},
		})
	}

	ing := &networkv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ghostblog.Name,
			Namespace:   ghostblog.Namespace,
			Annotations: annotations,
		},
		Spec: networkv1.IngressSpec{
			Rules: rules,
			TLS:   tls,
		},
	}

	// Set the ownerRef for the Ingress
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(ghostblog, ing, r.Scheme); err != nil {
		return nil, err
	}
	return ing, nil
}

// labelsForGhostBlog returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForGhostBlog(name string, imageFromCrd string) map[string]string {
	var imageTag string
	image, err := imageForGhostBlog(imageFromCrd)
	if err == nil {
		imageTag = strings.Split(image, ":")[1]
	}
	return map[string]string{"app.kubernetes.io/name": "GhostBlog",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/version":    imageTag,
		"app.kubernetes.io/part-of":    "cms-operator",
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

// imageForGhostBlog gets the Operand image which is managed by this controller
// from the GHOSTBLOG_IMAGE environment variable defined in the config/manager/manager.yaml
func imageForGhostBlog(imageFromCrd string) (string, error) {
	image := "ghost:latest"

	if imageFromCrd != "" {
		image = imageFromCrd
	}

	var imageEnvVar = "GHOSTBLOG_IMAGE"
	imageFromEnv, found := os.LookupEnv(imageEnvVar)
	if found {
		image = imageFromEnv
	}

	return image, nil
}

func envVariablesForGhostBlog(ghostblog *cmsv1alpha1.GhostBlog) []corev1.EnvVar {
	var envs []corev1.EnvVar

	envs = append(envs, corev1.EnvVar{
		Name:  "database__client",
		Value: ghostblog.Spec.Config.Database.Client,
	})

	if ghostblog.Spec.Config.Database.Connection.Filename != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "database__connection__filename",
			Value: ghostblog.Spec.Config.Database.Connection.Filename,
		})
	}

	if ghostblog.Spec.Config.URL != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "url",
			Value: ghostblog.Spec.Config.URL,
		})
	}

	return envs
}

func volumeForGhostBlog(ghostblog *cmsv1alpha1.GhostBlog) []corev1.Volume {
	var volumes []corev1.Volume

	if ghostblog.Spec.Persistent.Enabled {
		volumes = append(volumes, corev1.Volume{
			Name: "ghostblog-data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: ghostblog.Name + "-pvc",
				},
			},
		})
	}

	return volumes
}

func volumeMountsForGhostBlog(ghostblog *cmsv1alpha1.GhostBlog) []corev1.VolumeMount {
	var volumeMounts []corev1.VolumeMount

	if ghostblog.Spec.Persistent.Enabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "ghostblog-data",
			MountPath: "/var/lib/ghost/content",
		})
	}

	return volumeMounts
}

// SetupWithManager sets up the controller with the Manager.
func (r *GhostBlogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cmsv1alpha1.GhostBlog{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Owns(&networkv1.Ingress{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}
