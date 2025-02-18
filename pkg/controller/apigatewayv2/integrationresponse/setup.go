/*
Copyright 2020 The Crossplane Authors.

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

package integrationresponse

import (
	"context"

	svcsdk "github.com/aws/aws-sdk-go/service/apigatewayv2"
	ctrl "sigs.k8s.io/controller-runtime"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	svcapitypes "github.com/crossplane/provider-aws/apis/apigatewayv2/v1alpha1"
	aws "github.com/crossplane/provider-aws/pkg/clients"
)

// SetupIntegrationResponse adds a controller that reconciles IntegrationResponse.
func SetupIntegrationResponse(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(svcapitypes.IntegrationResponseGroupKind)
	opts := []option{
		func(e *external) {
			e.preObserve = preObserve
			e.postObserve = postObserve
			e.preCreate = preCreate
			e.postCreate = postCreate
			e.preDelete = preDelete
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&svcapitypes.IntegrationResponse{}).
		Complete(managed.NewReconciler(mgr,
			resource.ManagedKind(svcapitypes.IntegrationResponseGroupVersionKind),
			managed.WithExternalConnecter(&connector{kube: mgr.GetClient(), opts: opts}),
			managed.WithInitializers(),
			managed.WithPollInterval(o.PollInterval),
			managed.WithLogger(o.Logger.WithValues("controller", name)),
			managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name)))))
}

func preObserve(_ context.Context, cr *svcapitypes.IntegrationResponse, obj *svcsdk.GetIntegrationResponseInput) error {
	obj.ApiId = cr.Spec.ForProvider.APIID
	obj.IntegrationId = cr.Spec.ForProvider.IntegrationID
	obj.IntegrationResponseId = aws.String(meta.GetExternalName(cr))
	return nil
}

func postObserve(_ context.Context, cr *svcapitypes.IntegrationResponse, _ *svcsdk.GetIntegrationResponseOutput, obs managed.ExternalObservation, err error) (managed.ExternalObservation, error) {
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	cr.SetConditions(xpv1.Available())
	return obs, nil
}
func preCreate(_ context.Context, cr *svcapitypes.IntegrationResponse, obj *svcsdk.CreateIntegrationResponseInput) error {
	obj.ApiId = cr.Spec.ForProvider.APIID
	obj.IntegrationId = cr.Spec.ForProvider.IntegrationID
	return nil
}
func postCreate(_ context.Context, cr *svcapitypes.IntegrationResponse, resp *svcsdk.CreateIntegrationResponseOutput, cre managed.ExternalCreation, err error) (managed.ExternalCreation, error) {
	if err != nil {
		return managed.ExternalCreation{}, err
	}
	meta.SetExternalName(cr, aws.StringValue(resp.IntegrationResponseId))
	return cre, nil
}

func preDelete(_ context.Context, cr *svcapitypes.IntegrationResponse, obj *svcsdk.DeleteIntegrationResponseInput) (bool, error) {
	obj.ApiId = cr.Spec.ForProvider.APIID
	obj.IntegrationId = cr.Spec.ForProvider.IntegrationID
	obj.IntegrationResponseId = aws.String(meta.GetExternalName(cr))
	return false, nil
}
