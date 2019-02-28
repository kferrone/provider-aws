/*
Copyright 2018 The Crossplane Authors.

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

package cache

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/go-test/deep"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplaneio/crossplane/pkg/apis/aws/cache/v1alpha1"
	awsv1alpha1 "github.com/crossplaneio/crossplane/pkg/apis/aws/v1alpha1"
	corev1alpha1 "github.com/crossplaneio/crossplane/pkg/apis/core/v1alpha1"
	elasticacheclient "github.com/crossplaneio/crossplane/pkg/clients/aws/elasticache"
	"github.com/crossplaneio/crossplane/pkg/clients/aws/elasticache/fake"
	"github.com/crossplaneio/crossplane/pkg/test"
)

const (
	namespace   = "coolNamespace"
	name        = "coolGroup"
	uid         = types.UID("definitely-a-uuid")
	id          = elasticacheclient.NamePrefix + "-efdd8494195d7940" // FNV-64a hash of uid
	description = "Crossplane managed " + v1alpha1.ReplicationGroupKindAPIVersion + " " + namespace + "/" + name

	cacheNodeType            = "n1.super.cool"
	atRestEncryptionEnabled  = true
	authToken                = "coolToken"
	autoFailoverEnabled      = true
	cacheParameterGroupName  = "coolParamGroup"
	cacheSubnetGroupName     = "coolSubnet"
	engineVersion            = "5.0.0"
	numCacheClusters         = 2
	numNodeGroups            = 2
	port                     = 6379
	host                     = "172.16.0.1"
	maintenanceWindow        = "tomorrow"
	replicasPerNodeGroup     = 2
	snapshotName             = "coolSnapshot"
	snapshotRetentionLimit   = 1
	snapshotWindow           = "thedayaftertomorrow"
	transitEncryptionEnabled = true

	cacheClusterID = id + "-0001"

	providerName       = "cool-aws"
	providerSecretName = "cool-aws-secret"
	providerSecretKey  = "credentials"
	providerSecretData = "definitelyini"

	connectionSecretName = "cool-connection-secret"
)

var (
	ctx       = context.Background()
	errorBoom = errors.New("boom")

	meta = metav1.ObjectMeta{Namespace: namespace, Name: name, UID: uid, Finalizers: []string{}}

	provider = awsv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: providerName},
		Spec: awsv1alpha1.ProviderSpec{
			Secret: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: providerSecretName},
				Key:                  providerSecretKey,
			},
		},
		Status: awsv1alpha1.ProviderStatus{
			ConditionedStatus: corev1alpha1.ConditionedStatus{
				Conditions: []corev1alpha1.Condition{{Type: corev1alpha1.Ready, Status: corev1.ConditionTrue}},
			},
		},
	}

	providerSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: providerSecretName},
		Data:       map[string][]byte{providerSecretKey: []byte(providerSecretData)},
	}
)

type replicationGroupModifier func(*v1alpha1.ReplicationGroup)

func withConditions(c ...corev1alpha1.Condition) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Status.ConditionedStatus.Conditions = c }
}

func withState(s string) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Status.State = s }
}

func withFinalizers(f ...string) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.ObjectMeta.Finalizers = f }
}

func withReclaimPolicy(p corev1alpha1.ReclaimPolicy) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Spec.ReclaimPolicy = p }
}

func withGroupName(n string) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Status.GroupName = n }
}

func withProviderID(id string) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Status.ProviderID = id }
}

func withEndpoint(e string) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Status.Endpoint = e }
}

func withPort(p int) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Status.Port = p }
}

func withDeletionTimestamp(t time.Time) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.ObjectMeta.DeletionTimestamp = &metav1.Time{Time: t} }
}

func withAuth() replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Spec.AuthEnabled = true }
}

func withClusterEnabled() replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Status.ClusterEnabled = true }
}

func withMemberClusters(members []string) replicationGroupModifier {
	return func(r *v1alpha1.ReplicationGroup) { r.Status.MemberClusters = members }
}

func replicationGroup(rm ...replicationGroupModifier) *v1alpha1.ReplicationGroup {
	r := &v1alpha1.ReplicationGroup{
		ObjectMeta: meta,
		Spec: v1alpha1.ReplicationGroupSpec{
			AutomaticFailoverEnabled:   autoFailoverEnabled,
			CacheNodeType:              cacheNodeType,
			CacheParameterGroupName:    cacheParameterGroupName,
			EngineVersion:              engineVersion,
			PreferredMaintenanceWindow: maintenanceWindow,
			SnapshotRetentionLimit:     snapshotRetentionLimit,
			SnapshotWindow:             snapshotWindow,
			TransitEncryptionEnabled:   transitEncryptionEnabled,
			ProviderRef:                corev1.LocalObjectReference{Name: providerName},
			ConnectionSecretRef:        corev1.LocalObjectReference{Name: connectionSecretName},
		},
	}

	for _, m := range rm {
		m(r)
	}

	return r
}

// Test that our Reconciler implementation satisfies the Reconciler interface.
var _ reconcile.Reconciler = &Reconciler{}

func TestCreate(t *testing.T) {
	cases := []struct {
		name        string
		csdk        createsyncdeletekeyer
		r           *v1alpha1.ReplicationGroup
		want        *v1alpha1.ReplicationGroup
		wantRequeue bool
	}{
		{
			name: "SuccessfulCreate",
			csdk: &elastiCache{client: &fake.MockClient{
				MockCreateReplicationGroupRequest: func(_ *elasticache.CreateReplicationGroupInput) elasticache.CreateReplicationGroupRequest {
					return elasticache.CreateReplicationGroupRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Data: &elasticache.CreateReplicationGroupOutput{}},
					}
				},
			}},
			r: replicationGroup(withAuth()),
			want: replicationGroup(
				withAuth(),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue}),
				withFinalizers(finalizerName),
				withGroupName(id),
			),
			wantRequeue: true,
		},
		{
			name: "FailedCreate",
			csdk: &elastiCache{client: &fake.MockClient{
				MockCreateReplicationGroupRequest: func(_ *elasticache.CreateReplicationGroupInput) elasticache.CreateReplicationGroupRequest {
					return elasticache.CreateReplicationGroupRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Error: errorBoom},
					}
				},
			}},
			r: replicationGroup(),
			want: replicationGroup(withConditions(
				corev1alpha1.Condition{
					Type:    corev1alpha1.Failed,
					Status:  corev1.ConditionTrue,
					Reason:  reasonCreatingResource,
					Message: errorBoom.Error(),
				},
			)),
			wantRequeue: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRequeue := tc.csdk.Create(ctx, tc.r)

			if gotRequeue != tc.wantRequeue {
				t.Errorf("tc.csdk.Create(...): want: %t got: %t", tc.wantRequeue, gotRequeue)
			}

			if diff := deep.Equal(tc.want, tc.r); diff != nil {
				t.Errorf("r: want != got:\n%s", diff)
			}
		})
	}
}

func TestSync(t *testing.T) {
	cases := []struct {
		name        string
		csdk        createsyncdeletekeyer
		r           *v1alpha1.ReplicationGroup
		want        *v1alpha1.ReplicationGroup
		wantRequeue bool
	}{
		{
			name: "SuccessfulSyncWhileGroupCreating",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeReplicationGroupsOutput{
								ReplicationGroups: []elasticache.ReplicationGroup{{Status: aws.String(v1alpha1.StatusCreating)}},
							},
						},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(
					corev1alpha1.Condition{
						Type:    corev1alpha1.Failed,
						Status:  corev1.ConditionTrue,
						Reason:  reasonCreatingResource,
						Message: errorBoom.Error(),
					},
				),
			),
			want: replicationGroup(
				withState(v1alpha1.StatusCreating),
				withGroupName(name),
				withConditions(
					corev1alpha1.Condition{
						Type:    corev1alpha1.Failed,
						Status:  corev1.ConditionFalse,
						Reason:  reasonCreatingResource,
						Message: errorBoom.Error(),
					},
					corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue},
				),
			),
			wantRequeue: true,
		},
		{
			name: "SuccessfulSyncWhileGroupDeleting",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeReplicationGroupsOutput{
								ReplicationGroups: []elasticache.ReplicationGroup{{Status: aws.String(v1alpha1.StatusDeleting)}},
							},
						},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Deleting, Status: corev1.ConditionTrue}),
			),
			want: replicationGroup(
				withGroupName(name),
				withState(v1alpha1.StatusDeleting),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Deleting, Status: corev1.ConditionTrue}),
			),
			wantRequeue: false,
		},
		{
			name: "SuccessfulSyncWhileGroupModifying",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeReplicationGroupsOutput{
								ReplicationGroups: []elasticache.ReplicationGroup{{Status: aws.String(v1alpha1.StatusModifying)}},
							},
						},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Ready, Status: corev1.ConditionTrue}),
			),
			want: replicationGroup(
				withState(v1alpha1.StatusModifying),
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Ready, Status: corev1.ConditionFalse}),
			),
			wantRequeue: true,
		},
		{
			name: "SuccessfulSyncWhileGroupAvailableAndDoesNotNeedUpdate",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeReplicationGroupsOutput{
								ReplicationGroups: []elasticache.ReplicationGroup{{
									Status:                 aws.String(v1alpha1.StatusAvailable),
									MemberClusters:         []string{cacheClusterID},
									AutomaticFailover:      elasticache.AutomaticFailoverStatusEnabled,
									CacheNodeType:          aws.String(cacheNodeType),
									SnapshotRetentionLimit: aws.Int64(snapshotRetentionLimit),
									SnapshotWindow:         aws.String(snapshotWindow),
									ConfigurationEndpoint:  &elasticache.Endpoint{Address: aws.String(host), Port: aws.Int64(port)},
								}},
							},
						},
					}
				},
				MockDescribeCacheClustersRequest: func(_ *elasticache.DescribeCacheClustersInput) elasticache.DescribeCacheClustersRequest {
					return elasticache.DescribeCacheClustersRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeCacheClustersOutput{
								CacheClusters: []elasticache.CacheCluster{{
									EngineVersion:              aws.String(engineVersion),
									PreferredMaintenanceWindow: aws.String(maintenanceWindow),
								}},
							},
						},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue}),
			),
			want: replicationGroup(
				withState(v1alpha1.StatusAvailable),
				withGroupName(name),
				withConditions(
					corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionFalse},
					corev1alpha1.Condition{Type: corev1alpha1.Ready, Status: corev1.ConditionTrue},
				),
				withPort(port),
				withEndpoint(host),
				withMemberClusters([]string{cacheClusterID}),
			),
			wantRequeue: false,
		},
		{
			name: "SuccessfulSyncWhileGroupAvailableAndNeedsUpdate",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeReplicationGroupsOutput{
								ReplicationGroups: []elasticache.ReplicationGroup{{
									Status:                 aws.String(v1alpha1.StatusAvailable),
									MemberClusters:         []string{cacheClusterID},
									AutomaticFailover:      elasticache.AutomaticFailoverStatusDisabled, // This field needs updating.
									CacheNodeType:          aws.String(cacheNodeType),
									SnapshotRetentionLimit: aws.Int64(snapshotRetentionLimit),
									SnapshotWindow:         aws.String(snapshotWindow),
									ConfigurationEndpoint:  &elasticache.Endpoint{Address: aws.String(host), Port: aws.Int64(port)},
								}},
							},
						},
					}
				},
				MockDescribeCacheClustersRequest: func(_ *elasticache.DescribeCacheClustersInput) elasticache.DescribeCacheClustersRequest {
					return elasticache.DescribeCacheClustersRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeCacheClustersOutput{
								CacheClusters: []elasticache.CacheCluster{{
									EngineVersion:              aws.String(engineVersion),
									PreferredMaintenanceWindow: aws.String(maintenanceWindow),
								}},
							},
						},
					}
				},
				MockModifyReplicationGroupRequest: func(_ *elasticache.ModifyReplicationGroupInput) elasticache.ModifyReplicationGroupRequest {
					return elasticache.ModifyReplicationGroupRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Data: &elasticache.ModifyReplicationGroupOutput{}},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue}),
			),
			want: replicationGroup(
				withState(v1alpha1.StatusAvailable),
				withGroupName(name),
				withConditions(
					corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionFalse},
					corev1alpha1.Condition{Type: corev1alpha1.Ready, Status: corev1.ConditionTrue},
				),
				withPort(port),
				withEndpoint(host),
				withMemberClusters([]string{cacheClusterID}),
			),
			wantRequeue: false,
		},
		{
			name: "SuccessfulSyncWhileGroupAvailableAndCacheClustersNeedUpdate",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeReplicationGroupsOutput{
								ReplicationGroups: []elasticache.ReplicationGroup{{
									Status:                 aws.String(v1alpha1.StatusAvailable),
									MemberClusters:         []string{cacheClusterID},
									AutomaticFailover:      elasticache.AutomaticFailoverStatusEnabled,
									CacheNodeType:          aws.String(cacheNodeType),
									SnapshotRetentionLimit: aws.Int64(snapshotRetentionLimit),
									SnapshotWindow:         aws.String(snapshotWindow),
									ConfigurationEndpoint:  &elasticache.Endpoint{Address: aws.String(host), Port: aws.Int64(port)},
								}},
							},
						},
					}
				},
				MockDescribeCacheClustersRequest: func(_ *elasticache.DescribeCacheClustersInput) elasticache.DescribeCacheClustersRequest {
					return elasticache.DescribeCacheClustersRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeCacheClustersOutput{
								CacheClusters: []elasticache.CacheCluster{{
									EngineVersion:              aws.String(engineVersion),
									PreferredMaintenanceWindow: aws.String("never!"), // This field needs to be updated.
								}},
							},
						},
					}
				},
				MockModifyReplicationGroupRequest: func(_ *elasticache.ModifyReplicationGroupInput) elasticache.ModifyReplicationGroupRequest {
					return elasticache.ModifyReplicationGroupRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Data: &elasticache.ModifyReplicationGroupOutput{}},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue}),
			),
			want: replicationGroup(
				withState(v1alpha1.StatusAvailable),
				withGroupName(name),
				withConditions(
					corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionFalse},
					corev1alpha1.Condition{Type: corev1alpha1.Ready, Status: corev1.ConditionTrue},
				),
				withPort(port),
				withEndpoint(host),
				withMemberClusters([]string{cacheClusterID}),
			),
			wantRequeue: false,
		},
		{
			name: "FailedDescribeReplicationGroups",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Error: errorBoom},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue}),
			),
			want: replicationGroup(
				withGroupName(name),
				withConditions(
					corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue},
					corev1alpha1.Condition{
						Type:    corev1alpha1.Failed,
						Status:  corev1.ConditionTrue,
						Reason:  reasonSyncingResource,
						Message: errorBoom.Error(),
					},
				),
			),
			wantRequeue: true,
		},
		{
			name: "FailedDescribeCacheClusters",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeReplicationGroupsOutput{
								ReplicationGroups: []elasticache.ReplicationGroup{{
									Status:         aws.String(v1alpha1.StatusAvailable),
									MemberClusters: []string{cacheClusterID},
								}},
							},
						},
					}
				},
				MockDescribeCacheClustersRequest: func(_ *elasticache.DescribeCacheClustersInput) elasticache.DescribeCacheClustersRequest {
					return elasticache.DescribeCacheClustersRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Error: errorBoom},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue}),
			),
			want: replicationGroup(
				withState(v1alpha1.StatusAvailable),
				withGroupName(name),
				withConditions(
					corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionFalse},
					corev1alpha1.Condition{Type: corev1alpha1.Ready, Status: corev1.ConditionTrue},
					corev1alpha1.Condition{
						Type:    corev1alpha1.Failed,
						Status:  corev1.ConditionTrue,
						Reason:  reasonSyncingResource,
						Message: errors.Wrapf(errorBoom, "cannot describe cache cluster %s", cacheClusterID).Error(),
					},
				),
				withMemberClusters([]string{cacheClusterID}),
			),
			wantRequeue: true,
		},
		{
			name: "FailedModifyReplicationGroup",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDescribeReplicationGroupsRequest: func(_ *elasticache.DescribeReplicationGroupsInput) elasticache.DescribeReplicationGroupsRequest {
					return elasticache.DescribeReplicationGroupsRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeReplicationGroupsOutput{
								ReplicationGroups: []elasticache.ReplicationGroup{{
									Status:                 aws.String(v1alpha1.StatusAvailable),
									MemberClusters:         []string{cacheClusterID},
									AutomaticFailover:      elasticache.AutomaticFailoverStatusEnabled,
									CacheNodeType:          aws.String(cacheNodeType),
									SnapshotRetentionLimit: aws.Int64(snapshotRetentionLimit),
									SnapshotWindow:         aws.String(snapshotWindow),
									ConfigurationEndpoint:  &elasticache.Endpoint{Address: aws.String(host), Port: aws.Int64(port)},
								}},
							},
						},
					}
				},
				MockDescribeCacheClustersRequest: func(_ *elasticache.DescribeCacheClustersInput) elasticache.DescribeCacheClustersRequest {
					return elasticache.DescribeCacheClustersRequest{
						Request: &aws.Request{
							HTTPRequest: &http.Request{},
							Data: &elasticache.DescribeCacheClustersOutput{
								CacheClusters: []elasticache.CacheCluster{{
									EngineVersion:              aws.String(engineVersion),
									PreferredMaintenanceWindow: aws.String("never!"), // This field needs to be updated.
								}},
							},
						},
					}
				},
				MockModifyReplicationGroupRequest: func(_ *elasticache.ModifyReplicationGroupInput) elasticache.ModifyReplicationGroupRequest {
					return elasticache.ModifyReplicationGroupRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Error: errorBoom},
					}
				},
			}},
			r: replicationGroup(
				withGroupName(name),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionTrue}),
			),
			want: replicationGroup(
				withState(v1alpha1.StatusAvailable),
				withGroupName(name),
				withConditions(
					corev1alpha1.Condition{Type: corev1alpha1.Creating, Status: corev1.ConditionFalse},
					corev1alpha1.Condition{Type: corev1alpha1.Ready, Status: corev1.ConditionTrue},
					corev1alpha1.Condition{
						Type:    corev1alpha1.Failed,
						Status:  corev1.ConditionTrue,
						Reason:  reasonSyncingResource,
						Message: errorBoom.Error(),
					},
				),
				withPort(port),
				withEndpoint(host),
				withMemberClusters([]string{cacheClusterID}),
			),
			wantRequeue: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRequeue := tc.csdk.Sync(ctx, tc.r)

			if gotRequeue != tc.wantRequeue {
				t.Errorf("tc.csd.Sync(...): want: %t got: %t", tc.wantRequeue, gotRequeue)
			}

			if diff := deep.Equal(tc.want, tc.r); diff != nil {
				t.Errorf("r: want != got:\n%s", diff)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	cases := []struct {
		name        string
		csdk        createsyncdeletekeyer
		r           *v1alpha1.ReplicationGroup
		want        *v1alpha1.ReplicationGroup
		wantRequeue bool
	}{
		{
			name: "ReclaimRetainSuccessfulDelete",
			csdk: &elastiCache{},
			r:    replicationGroup(withFinalizers(finalizerName), withReclaimPolicy(corev1alpha1.ReclaimRetain)),
			want: replicationGroup(
				withReclaimPolicy(corev1alpha1.ReclaimRetain),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Deleting, Status: corev1.ConditionTrue}),
			),
			wantRequeue: false,
		},
		{
			name: "ReclaimDeleteSuccessfulDelete",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDeleteReplicationGroupRequest: func(_ *elasticache.DeleteReplicationGroupInput) elasticache.DeleteReplicationGroupRequest {
					return elasticache.DeleteReplicationGroupRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Data: &elasticache.DeleteReplicationGroupOutput{}},
					}
				},
			}},
			r: replicationGroup(withFinalizers(finalizerName), withReclaimPolicy(corev1alpha1.ReclaimDelete)),
			want: replicationGroup(
				withReclaimPolicy(corev1alpha1.ReclaimDelete),
				withConditions(corev1alpha1.Condition{Type: corev1alpha1.Deleting, Status: corev1.ConditionTrue}),
			),
			wantRequeue: false,
		},
		{
			name: "ReclaimDeleteFailedDelete",
			csdk: &elastiCache{client: &fake.MockClient{
				MockDeleteReplicationGroupRequest: func(_ *elasticache.DeleteReplicationGroupInput) elasticache.DeleteReplicationGroupRequest {
					return elasticache.DeleteReplicationGroupRequest{
						Request: &aws.Request{HTTPRequest: &http.Request{}, Error: errorBoom},
					}
				},
			}},
			r: replicationGroup(withFinalizers(finalizerName), withReclaimPolicy(corev1alpha1.ReclaimDelete)),
			want: replicationGroup(
				withFinalizers(finalizerName),
				withReclaimPolicy(corev1alpha1.ReclaimDelete),
				withConditions(
					corev1alpha1.Condition{
						Type:    corev1alpha1.Failed,
						Status:  corev1.ConditionTrue,
						Reason:  reasonDeletingResource,
						Message: errorBoom.Error(),
					},
				),
			),
			wantRequeue: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRequeue := tc.csdk.Delete(ctx, tc.r)

			if gotRequeue != tc.wantRequeue {
				t.Errorf("tc.csd.Delete(...): want: %t got: %t", tc.wantRequeue, gotRequeue)
			}

			if diff := deep.Equal(tc.want, tc.r); diff != nil {
				t.Errorf("r: want != got:\n%s", diff)
			}
		})
	}
}
func TestKey(t *testing.T) {
	cases := []struct {
		name string
		csdk createsyncdeletekeyer
		want string
	}{
		{
			name: "AuthTokenSet",
			csdk: &elastiCache{authToken: authToken},
			want: authToken,
		},
		{
			name: "AuthTokenUnset",
			csdk: &elastiCache{},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.csdk.Key()

			if got != tc.want {
				t.Errorf("tc.csd.Key(...): want: %s got: %s", tc.want, got)
			}
		})
	}
}

func TestConnect(t *testing.T) {
	cases := []struct {
		name    string
		conn    connecter
		i       *v1alpha1.ReplicationGroup
		want    createsyncdeletekeyer
		wantErr error
	}{
		{
			name: "SuccessfulConnect",
			conn: &providerConnecter{
				kube: &test.MockClient{MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
					switch key {
					case client.ObjectKey{Namespace: namespace, Name: providerName}:
						*obj.(*awsv1alpha1.Provider) = provider
					case client.ObjectKey{Namespace: namespace, Name: providerSecretName}:
						*obj.(*corev1.Secret) = providerSecret
					}
					return nil
				}},
				newClient: func(_ []byte, _ string) (elasticacheclient.Client, error) { return &fake.MockClient{}, nil },
			},
			i:    replicationGroup(),
			want: &elastiCache{client: &fake.MockClient{}},
		},
		{
			name: "FailedToGetProvider",
			conn: &providerConnecter{
				kube: &test.MockClient{MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
					return kerrors.NewNotFound(schema.GroupResource{}, providerName)
				}},
				newClient: func(_ []byte, _ string) (elasticacheclient.Client, error) { return &fake.MockClient{}, nil },
			},
			i:       replicationGroup(),
			wantErr: errors.WithStack(errors.Errorf("cannot get provider %s/%s:  \"%s\" not found", namespace, providerName, providerName)),
		},
		{
			name: "FailedToAssertProviderIsValid",
			conn: &providerConnecter{
				kube: &test.MockClient{MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
					// This provider does not have condition ready, and thus is
					// deemed invalid.
					*obj.(*awsv1alpha1.Provider) = awsv1alpha1.Provider{
						ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: providerName},
						Spec: awsv1alpha1.ProviderSpec{
							Secret: corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: providerSecretName},
								Key:                  providerSecretKey,
							},
						},
					}
					return nil
				}},
				newClient: func(_ []byte, _ string) (elasticacheclient.Client, error) { return &fake.MockClient{}, nil },
			},
			i:       replicationGroup(),
			wantErr: errors.Errorf("provider %s/%s is not ready", namespace, providerName),
		},
		{
			name: "FailedToGetProviderSecret",
			conn: &providerConnecter{
				kube: &test.MockClient{MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
					switch key {
					case client.ObjectKey{Namespace: namespace, Name: providerName}:
						*obj.(*awsv1alpha1.Provider) = provider
					case client.ObjectKey{Namespace: namespace, Name: providerSecretName}:
						return kerrors.NewNotFound(schema.GroupResource{}, providerSecretName)
					}
					return nil
				}},
				newClient: func(_ []byte, _ string) (elasticacheclient.Client, error) { return &fake.MockClient{}, nil },
			},
			i:       replicationGroup(),
			wantErr: errors.WithStack(errors.Errorf("cannot get provider secret %s/%s:  \"%s\" not found", namespace, providerSecretName, providerSecretName)),
		},
		{
			name: "FailedToCreateElastiCacheClient",
			conn: &providerConnecter{
				kube: &test.MockClient{MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
					switch key {
					case client.ObjectKey{Namespace: namespace, Name: providerName}:
						*obj.(*awsv1alpha1.Provider) = provider
					case client.ObjectKey{Namespace: namespace, Name: providerSecretName}:
						*obj.(*corev1.Secret) = providerSecret
					}
					return nil
				}},
				newClient: func(_ []byte, _ string) (elasticacheclient.Client, error) { return nil, errorBoom },
			},
			i:       replicationGroup(),
			want:    &elastiCache{},
			wantErr: errors.Wrap(errorBoom, "cannot create new AWS Replication Group client"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := tc.conn.Connect(ctx, tc.i)

			if diff := deep.Equal(tc.wantErr, gotErr); diff != nil {
				t.Errorf("tc.conn.Connect(...): want error != got error:\n%s", diff)
			}

			if diff := deep.Equal(tc.want, got); diff != nil {
				t.Errorf("tc.conn.Connect(...): want != got:\n%s", diff)
			}
		})
	}
}

type mockConnector struct {
	MockConnect func(ctx context.Context, i *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error)
}

func (c *mockConnector) Connect(ctx context.Context, i *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error) {
	return c.MockConnect(ctx, i)
}

type mockCSDK struct {
	MockCreate func(ctx context.Context, g *v1alpha1.ReplicationGroup) bool
	MockSync   func(ctx context.Context, g *v1alpha1.ReplicationGroup) bool
	MockDelete func(ctx context.Context, g *v1alpha1.ReplicationGroup) bool
	MockKey    func() string
}

func (csdk *mockCSDK) Create(ctx context.Context, g *v1alpha1.ReplicationGroup) bool {
	return csdk.MockCreate(ctx, g)
}

func (csdk *mockCSDK) Sync(ctx context.Context, g *v1alpha1.ReplicationGroup) bool {
	return csdk.MockSync(ctx, g)
}

func (csdk *mockCSDK) Delete(ctx context.Context, g *v1alpha1.ReplicationGroup) bool {
	return csdk.MockDelete(ctx, g)
}

func (csdk *mockCSDK) Key() string {
	return csdk.MockKey()
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		name    string
		rec     *Reconciler
		req     reconcile.Request
		want    reconcile.Result
		wantErr error
	}{
		{
			name: "SuccessfulDelete",
			rec: &Reconciler{
				connecter: &mockConnector{MockConnect: func(_ context.Context, _ *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error) {
					return &mockCSDK{MockDelete: func(_ context.Context, _ *v1alpha1.ReplicationGroup) bool { return false }}, nil
				}},
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						*obj.(*v1alpha1.ReplicationGroup) = *(replicationGroup(withGroupName(name), withDeletionTimestamp(time.Now())))
						return nil
					},
					MockUpdate: func(_ context.Context, _ runtime.Object) error { return nil },
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: false},
			wantErr: nil,
		},
		{
			name: "SuccessfulCreate",
			rec: &Reconciler{
				connecter: &mockConnector{MockConnect: func(_ context.Context, _ *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error) {
					return &mockCSDK{MockCreate: func(_ context.Context, _ *v1alpha1.ReplicationGroup) bool { return true }}, nil
				}},
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						*obj.(*v1alpha1.ReplicationGroup) = *(replicationGroup())
						return nil
					},
					MockUpdate: func(_ context.Context, _ runtime.Object) error { return nil },
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: true},
			wantErr: nil,
		},
		{
			name: "SuccessfulSync",
			rec: &Reconciler{
				connecter: &mockConnector{MockConnect: func(_ context.Context, _ *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error) {
					return &mockCSDK{
						MockSync: func(_ context.Context, _ *v1alpha1.ReplicationGroup) bool { return false },
						MockKey:  func() string { return "" },
					}, nil
				}},
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						switch key {
						case client.ObjectKey{Namespace: namespace, Name: name}:
							*obj.(*v1alpha1.ReplicationGroup) = *(replicationGroup(withGroupName(name), withEndpoint(host)))
						case client.ObjectKey{Namespace: namespace, Name: connectionSecretName}:
							return kerrors.NewNotFound(schema.GroupResource{}, connectionSecretName)
						}
						return nil
					},
					MockUpdate: func(_ context.Context, _ runtime.Object) error { return nil },
					MockCreate: func(_ context.Context, _ runtime.Object) error { return nil },
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: false},
			wantErr: nil,
		},
		{
			name: "FailedToGetNonexistentResource",
			rec: &Reconciler{
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						return kerrors.NewNotFound(schema.GroupResource{}, name)
					},
					MockUpdate: func(_ context.Context, _ runtime.Object) error { return nil },
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: false},
			wantErr: nil,
		},
		{
			name: "FailedToGetExtantResource",
			rec: &Reconciler{
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						return errorBoom
					},
					MockUpdate: func(_ context.Context, _ runtime.Object) error { return nil },
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: false},
			wantErr: errors.Wrapf(errorBoom, "cannot get resource %s/%s", namespace, name),
		},
		{
			name: "FailedToConnect",
			rec: &Reconciler{
				connecter: &mockConnector{MockConnect: func(_ context.Context, _ *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error) {
					return nil, errorBoom
				}},
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						*obj.(*v1alpha1.ReplicationGroup) = *(replicationGroup())
						return nil
					},
					MockUpdate: func(_ context.Context, obj runtime.Object) error {
						want := replicationGroup(withConditions(
							corev1alpha1.Condition{
								Type:    corev1alpha1.Failed,
								Status:  corev1.ConditionTrue,
								Reason:  reasonFetchingClient,
								Message: errorBoom.Error(),
							},
						))
						got := obj.(*v1alpha1.ReplicationGroup)
						if diff := deep.Equal(want, got); diff != nil {
							t.Errorf("kube.Update(...): want != got:\n%s", diff)
						}
						return nil
					},
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: true},
			wantErr: nil,
		},
		{
			name: "FailedToGetConnectionSecret",
			rec: &Reconciler{
				connecter: &mockConnector{MockConnect: func(_ context.Context, _ *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error) {
					return &mockCSDK{MockKey: func() string { return "" }}, nil
				}},
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						switch key {
						case types.NamespacedName{Namespace: namespace, Name: connectionSecretName}:
							return errorBoom
						case types.NamespacedName{Namespace: namespace, Name: name}:
							*obj.(*v1alpha1.ReplicationGroup) = *(replicationGroup(withGroupName(name)))
						}
						return nil
					},
					MockUpdate: func(_ context.Context, obj runtime.Object) error {
						want := replicationGroup(
							withGroupName(name),
							withConditions(
								corev1alpha1.Condition{
									Type:    corev1alpha1.Failed,
									Status:  corev1.ConditionTrue,
									Reason:  reasonSyncingSecret,
									Message: errors.Wrapf(errorBoom, "cannot get secret %s/%s", namespace, connectionSecretName).Error(),
								},
							))
						got := obj.(*v1alpha1.ReplicationGroup)
						if diff := deep.Equal(want, got); diff != nil {
							t.Errorf("kube.Update(...): want != got:\n%s", diff)
						}
						return nil
					},
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: true},
			wantErr: nil,
		},
		{
			name: "FailedToCreateConnectionSecret",
			rec: &Reconciler{
				connecter: &mockConnector{MockConnect: func(_ context.Context, _ *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error) {
					return &mockCSDK{MockKey: func() string { return "" }}, nil
				}},
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						switch key {
						case types.NamespacedName{Namespace: namespace, Name: connectionSecretName}:
							return kerrors.NewNotFound(schema.GroupResource{}, connectionSecretName)
						case types.NamespacedName{Namespace: namespace, Name: name}:
							*obj.(*v1alpha1.ReplicationGroup) = *(replicationGroup(withGroupName(name)))
						}
						return nil
					},
					MockUpdate: func(_ context.Context, obj runtime.Object) error {
						want := replicationGroup(
							withGroupName(name),
							withConditions(
								corev1alpha1.Condition{
									Type:    corev1alpha1.Failed,
									Status:  corev1.ConditionTrue,
									Reason:  reasonSyncingSecret,
									Message: errors.Wrapf(errorBoom, "cannot create secret %s/%s", namespace, connectionSecretName).Error(),
								},
							))
						got := obj.(*v1alpha1.ReplicationGroup)
						if diff := deep.Equal(want, got); diff != nil {
							t.Errorf("kube.Update(...): want != got:\n%s", diff)
						}
						return nil
					},
					MockCreate: func(_ context.Context, obj runtime.Object) error { return errorBoom },
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: true},
			wantErr: nil,
		},
		{
			name: "FailedToUpdateConnectionSecret",
			rec: &Reconciler{
				connecter: &mockConnector{MockConnect: func(_ context.Context, _ *v1alpha1.ReplicationGroup) (createsyncdeletekeyer, error) {
					return &mockCSDK{MockKey: func() string { return "" }}, nil
				}},
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						switch key {
						case types.NamespacedName{Namespace: namespace, Name: connectionSecretName}:
							return nil
						case types.NamespacedName{Namespace: namespace, Name: name}:
							*obj.(*v1alpha1.ReplicationGroup) = *(replicationGroup(withGroupName(name)))
						}
						return nil
					},
					MockUpdate: func(_ context.Context, obj runtime.Object) error {
						switch obj.(type) {
						case *corev1.Secret:
							return errorBoom
						case *v1alpha1.ReplicationGroup:
							want := replicationGroup(
								withGroupName(name),
								withConditions(
									corev1alpha1.Condition{
										Type:    corev1alpha1.Failed,
										Status:  corev1.ConditionTrue,
										Reason:  reasonSyncingSecret,
										Message: errors.Wrapf(errorBoom, "cannot update secret %s/%s", namespace, connectionSecretName).Error(),
									},
								))
							got := obj.(*v1alpha1.ReplicationGroup)
							if diff := deep.Equal(want, got); diff != nil {
								t.Errorf("kube.Update(...): want != got:\n%s", diff)
							}
						}
						return nil
					},
				},
			},
			req:     reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
			want:    reconcile.Result{Requeue: true},
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotResult, gotErr := tc.rec.Reconcile(tc.req)

			if diff := deep.Equal(tc.wantErr, gotErr); diff != nil {
				t.Errorf("tc.rec.Reconcile(...): want error != got error:\n%s", diff)
			}

			if diff := deep.Equal(tc.want, gotResult); diff != nil {
				t.Errorf("tc.rec.Reconcile(...): want != got:\n%s", diff)
			}
		})
	}
}

func TestConnectionSecret(t *testing.T) {
	cases := []struct {
		name     string
		r        *v1alpha1.ReplicationGroup
		password string
		want     *corev1.Secret
	}{
		{
			name:     "Successful",
			r:        replicationGroup(withEndpoint(host)),
			password: authToken,
			want: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      connectionSecretName,
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: v1alpha1.APIVersion,
						Kind:       v1alpha1.ReplicationGroupKind,
						Name:       name,
						UID:        uid,
					}},
				},
				Data: map[string][]byte{
					corev1alpha1.ResourceCredentialsSecretEndpointKey: []byte(host),
					corev1alpha1.ResourceCredentialsSecretPasswordKey: []byte(authToken),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := connectionSecret(tc.r, tc.password)
			if diff := deep.Equal(tc.want, got); diff != nil {
				t.Errorf("connectionSecret(...): want != got:\n%s", diff)
			}
		})
	}
}
