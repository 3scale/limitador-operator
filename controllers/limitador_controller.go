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
	"github.com/3scale/limitador-operator/pkg/limitador"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	limitadorv1alpha1 "github.com/3scale/limitador-operator/api/v1alpha1"
)

// LimitadorReconciler reconciles a Limitador object
type LimitadorReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=limitador.3scale.net,resources=limitadors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=limitador.3scale.net,resources=limitadors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;delete

func (r *LimitadorReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("limitador", req.NamespacedName)

	// Delete Limitador deployment and service if needed
	limitadorObj := &limitadorv1alpha1.Limitador{}
	if err := r.Get(context.TODO(), req.NamespacedName, limitadorObj); err != nil {
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

	err := r.ensureLimitadorServiceExists(limitadorObj)
	reqLogger.V(1).Info("reconcile service", "error", err)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileDeployment(limitador.LimitadorDeployment(limitadorObj))
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

func (r *LimitadorReconciler) reconcileDeployment(desiredDeployment *v1.Deployment) error {
	currentDeployment := v1.Deployment{}
	key, _ := client.ObjectKeyFromObject(desiredDeployment)

	err := r.Get(context.TODO(), key, &currentDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(context.TODO(), desiredDeployment)
		} else {
			return err
		}
	}

	updated := false

	if currentDeployment.Spec.Replicas != desiredDeployment.Spec.Replicas {
		currentDeployment.Spec.Replicas = desiredDeployment.Spec.Replicas
		updated = true
	}

	if currentDeployment.Spec.Template.Spec.Containers[0].Image !=
		desiredDeployment.Spec.Template.Spec.Containers[0].Image {
		currentDeployment.Spec.Template.Spec.Containers[0].Image =
			desiredDeployment.Spec.Template.Spec.Containers[0].Image
		updated = true
	}

	if updated {
		return r.Update(context.TODO(), &currentDeployment)
	} else {
		return nil
	}
}

func (r *LimitadorReconciler) ensureLimitadorServiceExists(limitadorObj *limitadorv1alpha1.Limitador) error {
	limitadorService := limitador.LimitadorService(limitadorObj)
	limitadorServiceKey, _ := client.ObjectKeyFromObject(limitadorService)

	err := r.Get(context.TODO(), limitadorServiceKey, limitadorService)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(context.TODO(), limitadorService)
		} else {
			return err
		}
	}

	return nil
}
