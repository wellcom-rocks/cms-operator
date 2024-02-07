package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	//nolint:golint
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cmsv1alpha1 "github.com/wellcom-rocks/cms-operator/api/v1alpha1"
)

var _ = Describe("GhostBlog controller", func() {
	Context("GhostBlog controller test", func() {

		const GhostBlogName = "test-ghostblog"

		ctx := context.Background()

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GhostBlogName,
				Namespace: GhostBlogName,
			},
		}

		typeNamespaceName := types.NamespacedName{
			Name:      GhostBlogName,
			Namespace: GhostBlogName,
		}
		ghostblog := &cmsv1alpha1.GhostBlog{}

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			err := k8sClient.Create(ctx, namespace)
			Expect(err).To(Not(HaveOccurred()))

			By("Setting the Image ENV VAR which stores the Operand image")
			err = os.Setenv("GHOSTBLOG_IMAGE", "example.com/image:test")
			Expect(err).To(Not(HaveOccurred()))

			By("creating the custom resource for the Kind GhostBlog")
			err = k8sClient.Get(ctx, typeNamespaceName, ghostblog)
			if err != nil && errors.IsNotFound(err) {
				// Let's mock our custom resource at the same way that we would
				// apply on the cluster the manifest under config/samples
				ghostblog := &cmsv1alpha1.GhostBlog{
					ObjectMeta: metav1.ObjectMeta{
						Name:      GhostBlogName,
						Namespace: namespace.Name,
					},
					Spec: cmsv1alpha1.GhostBlogSpec{
						Size:          1,
						ContainerPort: 11211,
						Config: cmsv1alpha1.GhostConfigSpec{
							URL: "http://example.com",
							Database: cmsv1alpha1.GhostDatabaseSpec{
								Client: "sqlite3",
								Connection: cmsv1alpha1.GhostDatabaseConnectionSpec{
									Filename: "/var/lib/ghost/content/data/ghost.db",
								},
							},
						},
						Persistent: cmsv1alpha1.GhostPersistentSpec{
							Enabled: true,
							Size:    resource.MustParse("1Gi"),
						},
					},
				}

				err = k8sClient.Create(ctx, ghostblog)
				Expect(err).To(Not(HaveOccurred()))
			}
		})

		AfterEach(func() {
			By("removing the custom resource for the Kind GhostBlog")
			found := &cmsv1alpha1.GhostBlog{}
			err := k8sClient.Get(ctx, typeNamespaceName, found)
			Expect(err).To(Not(HaveOccurred()))

			Eventually(func() error {
				return k8sClient.Delete(context.TODO(), found)
			}, 2*time.Minute, time.Second).Should(Succeed())

			// TODO(user): Attention if you improve this code by adding other context test you MUST
			// be aware of the current delete namespace limitations.
			// More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations
			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, namespace)

			By("Removing the Image ENV VAR which stores the Operand image")
			_ = os.Unsetenv("GHOSTBLOG_IMAGE")
		})

		It("should successfully reconcile a custom resource for GhostBlog", func() {
			By("Checking if the custom resource was successfully created")
			Eventually(func() error {
				found := &cmsv1alpha1.GhostBlog{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the custom resource created")
			ghostblogReconciler := &GhostBlogReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := ghostblogReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			By("Checking if Deployment was successfully created in the reconciliation")
			Eventually(func() error {
				found := &appsv1.Deployment{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the latest Status Condition added to the GhostBlog instance")
			Eventually(func() error {
				if ghostblog.Status.Conditions != nil &&
					len(ghostblog.Status.Conditions) != 0 {
					latestStatusCondition := ghostblog.Status.Conditions[len(ghostblog.Status.Conditions)-1]
					expectedLatestStatusCondition := metav1.Condition{
						Type:   typeAvailableGhostBlog,
						Status: metav1.ConditionTrue,
						Reason: "Reconciling",
						Message: fmt.Sprintf(
							"Deployment for custom resource (%s) with %d replicas created successfully",
							ghostblog.Name,
							ghostblog.Spec.Size),
					}
					if latestStatusCondition != expectedLatestStatusCondition {
						return fmt.Errorf("The latest status condition added to the GhostBlog instance is not as expected")
					}
				}
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})
