package deadmanssnitch

import (
	"context"

	"github.com/golang/mock/gomock"
	"k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	fakekubeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"testing"

	"github.com/openshift/deadmanssnitch-operator/config"
	"github.com/openshift/deadmanssnitch-operator/pkg/dmsclient"
	mockdms "github.com/openshift/deadmanssnitch-operator/pkg/dmsclient/mock"

	hiveapis "github.com/openshift/hive/pkg/apis"
	hivev1alpha1 "github.com/openshift/hive/pkg/apis/hive/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/types"
)

const (
	testClusterName         = "testCluster"
	testNamespace           = "testNamespace"
	testSnitchURL           = "https://deadmanssnitch.com/12345"
	testSnitchToken         = "abcdefg"
	testTag                 = "hive-test"
	testAPIKey              = "abc123"
	testOtherSyncSetPostfix = "-something-else"
)

type SyncSetEntry struct {
	name                     string
	snitchURL                string
	clusterDeploymentRefName string
}
type mocks struct {
	fakeKubeClient client.Client
	mockCtrl       *gomock.Controller
	mockDMSClient  *mockdms.MockClient
}

// setupDefaultMocks is an easy way to setup all of the default mocks
func setupDefaultMocks(t *testing.T, localObjects []runtime.Object) *mocks {
	mocks := &mocks{
		fakeKubeClient: fakekubeclient.NewFakeClient(localObjects...),
		mockCtrl:       gomock.NewController(t),
	}

	mocks.mockDMSClient = mockdms.NewMockClient(mocks.mockCtrl)

	return mocks
}

func rawToSecret(raw runtime.RawExtension) *corev1.Secret {
	decoder := scheme.Codecs.UniversalDecoder(corev1.SchemeGroupVersion)

	obj, _, err := decoder.Decode(raw.Raw, nil, nil)
	if err != nil {
		// okay, not everything in the syncset is necessarily a secret
		return nil
	}
	s, ok := obj.(*corev1.Secret)
	if ok {
		return s
	}

	return nil
}

// decode code to try to decode secret?  copied from somewhere to help..
func decode(t *testing.T, data []byte) (runtime.Object, metav1.Object, error) {
	decoder := scheme.Codecs.UniversalDecoder(corev1.SchemeGroupVersion)
	r, _, err := decoder.Decode(data, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	obj, err := meta.Accessor(r)
	if err != nil {
		return nil, nil, err
	}
	return r, obj, nil
}

// return a secret that matches the secret found in the hive namespace
func testSecret() *corev1.Secret {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeadMansSnitchAPISecretName,
			Namespace: DeadMansSnitchOperatorNamespace,
		},
		Data: map[string][]byte{
			DeadMansSnitchAPISecretKey: []byte(testAPIKey),
			DeadMansSnitchTagKey:       []byte(testTag),
		},
	}
	return s
}

// return a simple test ClusterDeployment
func testClusterDeployment() *hivev1alpha1.ClusterDeployment {
	labelMap := map[string]string{config.ClusterDeploymentManagedLabel: "true"}
	finalizers := []string{DeadMansSnitchFinalizer}

	cd := hivev1alpha1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testClusterName,
			Namespace:  testNamespace,
			Labels:     labelMap,
			Finalizers: finalizers,
		},
		Spec: hivev1alpha1.ClusterDeploymentSpec{
			ClusterName: testClusterName,
		},
	}
	cd.Status.Installed = true

	return &cd
}

// testSyncSet returns a SyncSet for an existing testClusterDeployment to use in testing.
func testSyncSet() *hivev1alpha1.SyncSet {
	return &hivev1alpha1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testClusterName + config.SyncSetPostfix,
			Namespace: testNamespace,
		},
		Spec: hivev1alpha1.SyncSetSpec{
			ClusterDeploymentRefs: []corev1.LocalObjectReference{
				{
					Name: testClusterName,
				},
			},
		},
	}
}

// testOtherSyncSet returns a SyncSet that is not for PD for an existing testClusterDeployment to use in testing.
func testOtherSyncSet() *hivev1alpha1.SyncSet {
	return &hivev1alpha1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testClusterName + testOtherSyncSetPostfix,
			Namespace: testNamespace,
		},
		Spec: hivev1alpha1.SyncSetSpec{
			ClusterDeploymentRefs: []corev1.LocalObjectReference{
				{
					Name: testClusterName,
				},
			},
		},
	}
}

// return a deleted ClusterDeployment
func deletedClusterDeployment() *hivev1alpha1.ClusterDeployment {
	cd := testClusterDeployment()
	now := metav1.Now()
	cd.DeletionTimestamp = &now

	return cd
}

// return a ClusterDeployment with Status.installed == false
func uninstalledClusterDeployment() *hivev1alpha1.ClusterDeployment {
	cd := testClusterDeployment()
	cd.Status.Installed = false
	cd.ObjectMeta.Finalizers = nil // operator will not have set a finalizer if it was never installed

	return cd
}

// return a ClusterDeployment with Label["managed"] == false
func nonManagedClusterDeployment() *hivev1alpha1.ClusterDeployment {
	labelMap := map[string]string{config.ClusterDeploymentManagedLabel: "false"}
	cd := hivev1alpha1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testClusterName,
			Namespace: testNamespace,
			Labels:    labelMap,
		},
		Spec: hivev1alpha1.ClusterDeploymentSpec{
			ClusterName: testClusterName,
		},
	}
	cd.Status.Installed = true

	return &cd
}

// return a ClusterDeployment with Label["noalerts"] == ""
func noalertsManagedClusterDeployment() *hivev1alpha1.ClusterDeployment {
	labelMap := map[string]string{
		config.ClusterDeploymentManagedLabel:  "true",
		config.ClusterDeploymentNoalertsLabel: "",
	}
	cd := hivev1alpha1.ClusterDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testClusterName,
			Namespace: testNamespace,
			Labels:    labelMap,
		},
		Spec: hivev1alpha1.ClusterDeploymentSpec{
			ClusterName: testClusterName,
		},
	}
	cd.Status.Installed = true

	return &cd
}

func TestReconcileClusterDeployment(t *testing.T) {
	hiveapis.AddToScheme(scheme.Scheme)
	tests := []struct {
		name             string
		localObjects     []runtime.Object
		expectedSyncSets *SyncSetEntry
		verifySyncSets   func(client.Client, *SyncSetEntry) bool
		setupDMSMock     func(*mockdms.MockClientMockRecorder)
	}{

		{
			name: "Test Creating",
			localObjects: []runtime.Object{
				testClusterDeployment(),
				testSecret(),
			},
			expectedSyncSets: &SyncSetEntry{
				name:                     testClusterName + config.SyncSetPostfix,
				snitchURL:                testSnitchURL,
				clusterDeploymentRefName: testClusterName,
			},
			verifySyncSets: verifySyncSetExists,
			setupDMSMock: func(r *mockdms.MockClientMockRecorder) {
				r.Create(gomock.Any()).Return(dmsclient.Snitch{CheckInURL: testSnitchURL, Tags: []string{testTag}}, nil).Times(1)
				r.FindSnitchesByName(gomock.Any()).Return([]dmsclient.Snitch{}, nil).Times(1)
				r.FindSnitchesByName(gomock.Any()).Return([]dmsclient.Snitch{
					{
						CheckInURL: testSnitchURL,
						Status:     "pending",
					},
				}, nil).Times(1)
				r.CheckIn(gomock.Any()).Return(nil).Times(1)
			},
		},
		{
			name: "Test Deleting",
			localObjects: []runtime.Object{
				deletedClusterDeployment(),
			},
			expectedSyncSets: &SyncSetEntry{},
			verifySyncSets:   verifyNoSyncSet,
			setupDMSMock: func(r *mockdms.MockClientMockRecorder) {
				r.Delete(gomock.Any()).Return(true, nil).Times(1)
				r.FindSnitchesByName(gomock.Any()).Return([]dmsclient.Snitch{
					{Token: testSnitchToken},
				}, nil).Times(1)
			},
		},
		{
			name: "Test ClusterDeployment Status.Installed == false",
			localObjects: []runtime.Object{
				uninstalledClusterDeployment(),
			},
			expectedSyncSets: &SyncSetEntry{},
			verifySyncSets:   verifyNoSyncSet,
			setupDMSMock: func(r *mockdms.MockClientMockRecorder) {
			},
		},
		{
			name: "Test Non managed ClusterDeployment",
			localObjects: []runtime.Object{
				nonManagedClusterDeployment(),
			},
			expectedSyncSets: &SyncSetEntry{},
			verifySyncSets:   verifyNoSyncSet,
			setupDMSMock: func(r *mockdms.MockClientMockRecorder) {
			},
		},
		{
			name: "Test Create Managed ClusterDeployment with Alerts disabled",
			localObjects: []runtime.Object{
				noalertsManagedClusterDeployment(),
			},
			expectedSyncSets: &SyncSetEntry{},
			verifySyncSets:   verifyNoSyncSet,
			setupDMSMock: func(r *mockdms.MockClientMockRecorder) {
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// ARRANGE
			mocks := setupDefaultMocks(t, test.localObjects)
			test.setupDMSMock(mocks.mockDMSClient.EXPECT())

			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			// after mocks is defined
			defer mocks.mockCtrl.Finish()

			rdms := &ReconcileDeadMansSnitch{
				client:    mocks.fakeKubeClient,
				scheme:    scheme.Scheme,
				dmsclient: mocks.mockDMSClient,
			}

			// ACT
			_, err := rdms.Reconcile(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      testClusterName,
					Namespace: testNamespace,
				},
			})

			// ASSERT
			//assert.Equal(t, test.expectedGetError, getErr)

			assert.NoError(t, err, "Unexpected Error")
			assert.True(t, test.verifySyncSets(mocks.fakeKubeClient, test.expectedSyncSets))
		})
	}
}

func TestRemoveAlertsAfterCreate(t *testing.T) {
	// test going from having alerts to not having alerts
	t.Run("Test Managed Cluster that later sets noalerts label", func(t *testing.T) {
		// ARRANGE
		mocks := setupDefaultMocks(t, []runtime.Object{
			testClusterDeployment(),
			testSecret(),
			testSyncSet(),
			testOtherSyncSet(),
		})
		//test.setupDMSMock(mocks.mockDMSClient.EXPECT())
		setupDMSMock :=
			func(r *mockdms.MockClientMockRecorder) {
				r.Delete(gomock.Any()).Return(true, nil).Times(1)
				r.FindSnitchesByName(gomock.Any()).Return([]dmsclient.Snitch{
					{Token: testSnitchToken},
				}, nil).Times(1)
			}

		setupDMSMock(mocks.mockDMSClient.EXPECT())

		// This is necessary for the mocks to report failures like methods not being called an expected number of times.
		// after mocks is defined
		defer mocks.mockCtrl.Finish()

		rdms := &ReconcileDeadMansSnitch{
			client:    mocks.fakeKubeClient,
			scheme:    scheme.Scheme,
			dmsclient: mocks.mockDMSClient,
		}

		// ACT (create)
		_, err := rdms.Reconcile(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      testClusterName,
				Namespace: testNamespace,
			},
		})

		// UPDATE (noalerts)
		// can't set to empty string, it won't update.. value does not matter
		clusterDeployment := &hivev1alpha1.ClusterDeployment{}
		err = mocks.fakeKubeClient.Get(context.TODO(), types.NamespacedName{Namespace: testNamespace, Name: testClusterName}, clusterDeployment)
		clusterDeployment.Labels[config.ClusterDeploymentNoalertsLabel] = "X"
		err = mocks.fakeKubeClient.Update(context.TODO(), clusterDeployment)

		// Act (delete) [2x because was seeing other SyncSet's getting deleted]
		_, err = rdms.Reconcile(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      testClusterName,
				Namespace: testNamespace,
			},
		})
		_, err = rdms.Reconcile(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      testClusterName,
				Namespace: testNamespace,
			},
		})
		_, err = rdms.Reconcile(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      testClusterName,
				Namespace: testNamespace,
			},
		})
		_, err = rdms.Reconcile(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      testClusterName,
				Namespace: testNamespace,
			},
		})

		// ASSERT (no unexpected syncset)
		assert.NoError(t, err, "Unexpected Error")
		assert.True(t, verifyNoSyncSet(mocks.fakeKubeClient, &SyncSetEntry{}))
		// verify the "other" syncset didn't get deleted
		assert.True(t, verifyOtherSyncSetExists(mocks.fakeKubeClient, &SyncSetEntry{}))
	})
}

func verifySyncSetExists(c client.Client, expected *SyncSetEntry) bool {
	ss := hivev1alpha1.SyncSet{}
	err := c.Get(context.TODO(),
		types.NamespacedName{Name: expected.name, Namespace: testNamespace},
		&ss)

	if err != nil {
		return false
	}

	if expected.name != ss.Name {
		return false
	}

	if expected.clusterDeploymentRefName != ss.Spec.ClusterDeploymentRefs[0].Name {
		return false
	}
	secret := rawToSecret(ss.Spec.Resources[0])
	if secret == nil {
		return false
	}

	return string(secret.Data[config.KeySnitchURL]) == expected.snitchURL
}

func verifyNoSyncSet(c client.Client, expected *SyncSetEntry) bool {
	ssList := &hivev1alpha1.SyncSetList{}
	opts := client.ListOptions{Namespace: testNamespace}
	err := c.List(context.TODO(), &opts, ssList)

	if err != nil {
		if errors.IsNotFound(err) {
			// no syncsets are defined, this is OK
			return true
		}
	}

	for _, ss := range ssList.Items {
		if ss.Name != testClusterName+testOtherSyncSetPostfix {
			// too bad, found a syncset associated with this operator
			return false
		}
	}

	// if we got here, it's good.  list was empty or everything passed
	return true
}

// verifyOtherSyncSetExists verifies that there is the "other" SyncSet present
func verifyOtherSyncSetExists(c client.Client, expected *SyncSetEntry) bool {
	ssList := &hivev1alpha1.SyncSetList{}
	opts := client.ListOptions{Namespace: testNamespace}
	err := c.List(context.TODO(), &opts, ssList)

	if err != nil {
		if errors.IsNotFound(err) {
			// no syncsets are defined, this is bad
			return false
		}
	}

	found := false
	for _, ss := range ssList.Items {
		if ss.Name == testClusterName+testOtherSyncSetPostfix {
			// too bad, found a syncset associated with this operator
			found = true
		}
	}

	return found
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
