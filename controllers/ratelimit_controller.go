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
	"net/url"
	"strconv"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	limitadorv1alpha1 "github.com/3scale/limitador-operator/api/v1alpha1"
	"github.com/3scale/limitador-operator/pkg/helpers"
	"github.com/3scale/limitador-operator/pkg/limitador"
	"github.com/3scale/limitador-operator/pkg/reconcilers"
)

const rateLimitFinalizer = "finalizer.ratelimit.limitador.3scale.net"

// Assumes that there's only one Limitador per namespace. We might want to
// change this in the future.
type LimitadorServiceDiscovery interface {
	URL(namespace string) (*url.URL, error)
}

type defaultLimitadorServiceDiscovery struct{}

func (d *defaultLimitadorServiceDiscovery) URL(namespace string) (*url.URL, error) {
	serviceUrl := "http://" + limitador.ServiceName + "." + namespace + ".svc.cluster.local:" +
		strconv.Itoa(limitador.ServiceHTTPPort)

	return url.Parse(serviceUrl)
}

// RateLimitReconciler reconciles a RateLimit object
type RateLimitReconciler struct {
	*reconcilers.BaseReconciler
	LimitadorDiscovery LimitadorServiceDiscovery
}

// +kubebuilder:rbac:groups=limitador.3scale.net,resources=ratelimits,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=limitador.3scale.net,resources=ratelimits/status,verbs=get;update;patch

func (r *RateLimitReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Logger().WithValues("ratelimit", req.NamespacedName)
	reqLogger.V(1).Info("Reconciling RateLimit")

	limit := &limitadorv1alpha1.RateLimit{}
	if err := r.Client().Get(ctx, req.NamespacedName, limit); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		reqLogger.Error(err, "Failed to get RateLimit object.")
		return ctrl.Result{}, err
	}

	isLimitMarkedToBeDeleted := limit.GetDeletionTimestamp() != nil
	if isLimitMarkedToBeDeleted {
		if helpers.Contains(limit.GetFinalizers(), rateLimitFinalizer) {
			if err := r.finalizeRateLimit(limit); err != nil {
				return ctrl.Result{}, err
			}

			// Remove finalizer. Once all finalizers have been removed, the
			// object will be deleted.
			controllerutil.RemoveFinalizer(limit, rateLimitFinalizer)
			if err := r.Client().Update(ctx, limit); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	if err := r.ensureFinalizerIsAdded(ctx, limit, reqLogger); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.createLimitInLimitador(limit); err != nil {
		reqLogger.Error(err, "Failed to create rate limit in Limitador.")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RateLimitReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&limitadorv1alpha1.RateLimit{}).
		WithEventFilter(r.updateLimitPredicate()).
		Complete(r)
}

// This should be temporary. This is not how a filter should be used. However,
// with the current Limitador API, when updating a limit, we need both the
// current and the previous version. After updating the Limitador API to work
// with IDs, this won't be needed.
func (r *RateLimitReconciler) updateLimitPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldVersion := e.ObjectOld.(*limitadorv1alpha1.RateLimit)
			newVersion := e.ObjectNew.(*limitadorv1alpha1.RateLimit)

			if oldVersion.ObjectMeta.Generation == newVersion.ObjectMeta.Generation {
				return false
			}

			// The namespace should be the same in the old and the new version,
			// so we can use either.
			limitadorUrl, err := r.limitadorDiscovery().URL(newVersion.Namespace)
			if err != nil {
				return false
			}

			limitadorClient := limitador.NewClient(*limitadorUrl)

			// Try to create the new version even if the old one can't be
			// deleted. This might leave in Limitador limits that should no
			// longer be there. As this function should only be temporary this
			// should be fine for a first version of the controller.
			_ = limitadorClient.DeleteLimit(&oldVersion.Spec)

			return true
		},
	}
}

func (r *RateLimitReconciler) createLimitInLimitador(limit *limitadorv1alpha1.RateLimit) error {
	limitadorUrl, err := r.limitadorDiscovery().URL(limit.Namespace)
	if err != nil {
		return err
	}

	limitadorClient := limitador.NewClient(*limitadorUrl)
	return limitadorClient.CreateLimit(&limit.Spec)
}

func (r *RateLimitReconciler) ensureFinalizerIsAdded(ctx context.Context, limit *limitadorv1alpha1.RateLimit, reqLogger logr.Logger) error {
	numberOfFinalizers := len(limit.GetFinalizers())
	controllerutil.AddFinalizer(limit, rateLimitFinalizer)
	if numberOfFinalizers == len(limit.GetFinalizers()) {
		// The finalizer was already there, no need to update
		return nil
	}

	if err := r.Client().Update(ctx, limit); err != nil {
		reqLogger.Error(err, "Failed to update the rate limit with finalizer")
		return err
	}

	return nil
}

func (r *RateLimitReconciler) finalizeRateLimit(rateLimit *limitadorv1alpha1.RateLimit) error {
	limitadorUrl, err := r.limitadorDiscovery().URL(rateLimit.Namespace)
	if err != nil {
		return err
	}

	limitadorClient := limitador.NewClient(*limitadorUrl)
	return limitadorClient.DeleteLimit(&rateLimit.Spec)
}

func (r *RateLimitReconciler) limitadorDiscovery() LimitadorServiceDiscovery {
	if r.LimitadorDiscovery == nil {
		r.LimitadorDiscovery = &defaultLimitadorServiceDiscovery{}
	}

	return r.LimitadorDiscovery
}
