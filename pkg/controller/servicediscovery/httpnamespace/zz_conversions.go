/*
Copyright 2021 The Crossplane Authors.

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

// Code generated by ack-generate. DO NOT EDIT.

package httpnamespace

import (
	"github.com/aws/aws-sdk-go/aws/awserr"
	svcsdk "github.com/aws/aws-sdk-go/service/servicediscovery"

	svcapitypes "github.com/crossplane/provider-aws/apis/servicediscovery/v1alpha1"
)

// NOTE(muvaf): We return pointers in case the function needs to start with an
// empty object, hence need to return a new pointer.

// GenerateCreateHttpNamespaceInput returns a create input.
func GenerateCreateHttpNamespaceInput(cr *svcapitypes.HTTPNamespace) *svcsdk.CreateHttpNamespaceInput {
	res := &svcsdk.CreateHttpNamespaceInput{}

	if cr.Spec.ForProvider.Description != nil {
		res.SetDescription(*cr.Spec.ForProvider.Description)
	}
	if cr.Spec.ForProvider.Name != nil {
		res.SetName(*cr.Spec.ForProvider.Name)
	}
	if cr.Spec.ForProvider.Tags != nil {
		f2 := []*svcsdk.Tag{}
		for _, f2iter := range cr.Spec.ForProvider.Tags {
			f2elem := &svcsdk.Tag{}
			if f2iter.Key != nil {
				f2elem.SetKey(*f2iter.Key)
			}
			if f2iter.Value != nil {
				f2elem.SetValue(*f2iter.Value)
			}
			f2 = append(f2, f2elem)
		}
		res.SetTags(f2)
	}

	return res
}

// GenerateUpdateHttpNamespaceInput returns an update input.
func GenerateUpdateHttpNamespaceInput(cr *svcapitypes.HTTPNamespace) *svcsdk.UpdateHttpNamespaceInput {
	res := &svcsdk.UpdateHttpNamespaceInput{}

	return res
}

// IsNotFound returns whether the given error is of type NotFound or not.
func IsNotFound(err error) bool {
	awsErr, ok := err.(awserr.Error)
	return ok && awsErr.Code() == "UNKNOWN"
}
