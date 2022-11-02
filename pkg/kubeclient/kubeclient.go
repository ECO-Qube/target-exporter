package kubeclient

import (
	"context"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	//
	// Uncomment to load all auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth"
	//
	// Or uncomment to load specific auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

type Kubeclient struct {
	*kubernetes.Clientset

	logger *zap.Logger
}

func NewKubeClient(client *kubernetes.Clientset, logger *zap.Logger) *Kubeclient {
	return &Kubeclient{client, logger}
}

// https://github.com/kubernetes/client-go/blob/master/examples/out-of-cluster-client-configuration/main.go
func (kubeclient *Kubeclient) GetNodeList() (*v1.PodList, error) {
	// TODO: Make namespace configurable or get via label selection
	pods, err := kubeclient.CoreV1().Pods("kube-system").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		kubeclient.logger.Error("Error getting pods", zap.Error(err))
		return nil, err
	}
	return pods, nil

	// Examples for error handling:
	// - Use helper functions like e.g. errors.IsNotFound()
	// - And/or cast to StatusError and use its properties like e.g. ErrStatus.Message
	//namespace := "default"
	//pod := "example-xxxxx"
	//_, err = clientset.CoreV1().Pods(namespace).Get(context.TODO(), pod, metav1.GetOptions{})
	//if errors.IsNotFound(err) {
	//	fmt.Printf("Pod %s in namespace %s not found\n", pod, namespace)
	//} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
	//	fmt.Printf("Error getting pod %s in namespace %s: %v\n",
	//		pod, namespace, statusError.ErrStatus.Message)
	//} else if err != nil {
	//	panic(err.Error())
	//} else {
	//	fmt.Printf("Found pod %s in namespace %s\n", pod, namespace)
	//}
}
