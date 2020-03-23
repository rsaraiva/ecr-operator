package ecr

import (
	"context"
	"os"

	cachev1alpha1 "github.com/gympass/ecr-operator/pkg/apis/cache/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
)

var log = logf.Log.WithName("controller_ecr")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new ECR Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileECR{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("ecr-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ECR
	err = c.Watch(&source.Kind{Type: &cachev1alpha1.ECR{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner ECR
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &cachev1alpha1.ECR{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileECR implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileECR{}

// ReconcileECR reconciles a ECR object
type ReconcileECR struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ECR object and makes changes based on the state read
// and what is in the ECR.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileECR) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ECR")

	// Fetch the ECR instance
	instance := &cachev1alpha1.ECR{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("ECR resource not found. Ignoring since object must be deleted")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		reqLogger.Error(err, "Failed to get ECR")
		return reconcile.Result{}, err
	}

	// check if already exists
	repoExists, errAwsECRRepoExists := awsECRRepoExists(instance.Name)
	if errAwsECRRepoExists != nil {
		reqLogger.Error(errAwsECRRepoExists, "Error checking if repo exists")
		return reconcile.Result{}, errAwsECRRepoExists
	}
	if repoExists {
		reqLogger.Info("Skiping reconcile: ECR already exists", "ECR Name:", instance.Name)
		return reconcile.Result{}, nil
	}

	// Create a new ECR repository
	errCreateAwsECR := createAwsECR(instance, request)
	if errCreateAwsECR != nil {
		reqLogger.Error(errCreateAwsECR, "Error creating ECR repository!")
		return reconcile.Result{}, errCreateAwsECR
	}

	reqLogger.Info("ECR repository created!")
	return reconcile.Result{}, nil

	// Set ECR instance as the owner and controller
	// if err := controllerutil.SetControllerReference(instance, awsECR, r.scheme); err != nil {
	// 	return reconcile.Result{}, err
	// }

	// Check if this Pod already exists
	// found := &corev1.Pod{}
	// err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, found)
	// if err != nil && errors.IsNotFound(err) {
	// 	reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
	// 	err = r.client.Create(context.TODO(), pod)
	// 	if err != nil {
	// 		return reconcile.Result{}, err
	// 	}

	// 	// Pod created successfully - don't requeue
	// 	return reconcile.Result{}, nil
	// } else if err != nil {
	// 	return reconcile.Result{}, err
	// }

	// Pod already exists - don't requeue
	// reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	// return reconcile.Result{}, nil
}

func createAwsECR(instance *cachev1alpha1.ECR, request reconcile.Request) error {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Creating ECR")

	config := &aws.Config{Region: aws.String(getAwsRegion())}
	svc := ecr.New(session.New(), config)

	input := &ecr.CreateRepositoryInput{
		RepositoryName: aws.String(instance.Name),
	}
	output, err := svc.CreateRepository(input)

	if err != nil {
		reqLogger.Error(err, "\nError creating the repo %v in region %v\n", instance.Name, aws.StringValue(config.Region))
		return err
	}

	reqLogger.Info("\nECR Repository \"%v\" created successfully!\n\nAWS Output:\n%v", instance.Name, output)
	return nil
}

// Returns the aws region from env var or default region defined in DEAFULT_AWS_REGION constant
func getAwsRegion() string {
	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion != "" {
		return awsRegion
	}
	return "us-east-1"
}

func awsECRRepoExists(repoName string) (bool, error) {
	config := &aws.Config{Region: aws.String(getAwsRegion())}
	svc := ecr.New(session.New(), config)

	input := &ecr.DescribeRepositoriesInput{
		RepositoryNames: []*string{aws.String(repoName)},
	}

	_, err := svc.DescribeRepositories(input)

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case ecr.ErrCodeRepositoryNotFoundException:
				return false, nil
			default:
				return false, err
			}
		} else {
			return false, err
		}
	}

	return false, nil
}
