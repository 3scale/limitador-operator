/*
Copyright 2020 Red Hat.

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

package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	limitadorv1alpha1 "github.com/3scale/limitador-operator/api/v1alpha1"
	"github.com/3scale/limitador-operator/pkg/limitador"
	"github.com/3scale/limitador-operator/pkg/reconcilers"
)

// LimitadorReconciler reconciles a Limitador object
type LimitadorReconciler struct {
	*reconcilers.BaseReconciler
}

// +kubebuilder:rbac:groups=limitador.3scale.net,resources=limitadors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=limitador.3scale.net,resources=limitadors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;delete

func (r *LimitadorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Logger().WithValues("limitador", req.NamespacedName)
	reqLogger.V(1).Info("Reconciling Limitador")

	// Delete Limitador deployment and service if needed
	limitadorObj := &limitadorv1alpha1.Limitador{}
	if err := r.Client().Get(ctx, req.NamespacedName, limitadorObj); err != nil {
		if errors.IsNotFound(err) {
			// The deployment and the service should be deleted automatically
			// because they have an owner ref to Limitador
			return ctrl.Result{}, nil
		} else {
			reqLogger.Error(err, "Failed to get Limitador object.")
			return ctrl.Result{}, err
		}
	}

	if limitadorObj.GetDeletionTimestamp() != nil { // Marked to be deleted
		reqLogger.V(1).Info("marked to be deleted")
		return ctrl.Result{}, nil
	}

	limitadorService := limitador.LimitadorService(limitadorObj)
	err := r.ReconcileService(ctx, limitadorService, reconcilers.CreateOnlyMutator)
	reqLogger.V(1).Info("reconcile service", "error", err)
	if err != nil {
		return ctrl.Result{}, err
	}

	deployment := limitador.LimitadorDeployment(limitadorObj)
	err = r.ReconcileDeployment(ctx, deployment, mutateLimitadorDeployment)
	reqLogger.V(1).Info("reconcile deployment", "error", err)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LimitadorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&limitadorv1alpha1.Limitador{}).
		Complete(r)
}

func mutateLimitadorDeployment(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*appsv1.Deployment)
	if !ok {
		return false, fmt.Errorf("%T is not a *appsv1.Deployment", existingObj)
	}
	desired, ok := desiredObj.(*appsv1.Deployment)
	if !ok {
		return false, fmt.Errorf("%T is not a *appsv1.Deployment", desiredObj)
	}

	updated := false

	if existing.Spec.Replicas != desired.Spec.Replicas {
		existing.Spec.Replicas = desired.Spec.Replicas
		updated = true
	}

	if existing.Spec.Template.Spec.Containers[0].Image != desired.Spec.Template.Spec.Containers[0].Image {
		existing.Spec.Template.Spec.Containers[0].Image = desired.Spec.Template.Spec.Containers[0].Image
		updated = true
	}

	return updated, nil
}
